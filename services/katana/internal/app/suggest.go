package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	katanav1 "github.com/0xHoaxen/shogun/gen/go/shogun/katana/v1"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/pkg/outbox"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
	"github.com/0xHoaxen/shogun/services/katana/internal/store"
	"github.com/0xHoaxen/shogun/services/katana/internal/store/db"
	"github.com/0xHoaxen/shogun/services/katana/internal/wire"
)

const (
	eventSource          = "katana"
	eventSuggestionReady = "profile.suggestion_ready"
	featureSuggest       = "katana.suggest"

	maxSuggestions = 5
	// knownWindow is how many of the newest suggestions a new one is compared
	// with, to avoid proposing the same edit twice.
	knownWindow = 100
)

// suggestSystem is the instruction for the model. The facts it is given are the
// only ground it may stand on.
const suggestSystem = `You help the owner keep their resume and LinkedIn profile current.
You are given facts from their GitHub activity and what they recently finished learning.
Propose at most 5 concrete edits.

Rules:
- Use only the given facts. Never invent employers, job titles, numbers, dates or technologies.
- Every suggestion must cite at least one link taken from the facts as evidence.
- "after" is text the owner could paste in as it is: first person, plain, no hype.
- "reason" says in one sentence what in the facts supports the edit.
- target is "resume" or "linkedin". section is one of headline, about, experience, skills, projects.

Answer with JSON only, no other text:
{"suggestions":[{"target":"resume","section":"projects","after":"...","reason":"...","evidence":[{"label":"...","url":"..."}]}]}
If the facts do not justify an edit, answer {"suggestions":[]}.`

// Completer is the part of *llm.Client that suggesting needs.
type Completer interface {
	Complete(ctx context.Context, feature string, req llm.Request) (llm.Response, error)
}

// ErrBadModelAnswer means the model's answer was not the JSON asked for. The
// run is retried, since a second answer may be fine.
var ErrBadModelAnswer = errors.New("app: model answer is not valid suggestions")

// ErrSuggestOff means no model or queue is set up for suggesting.
var ErrSuggestOff = errors.New("app: suggestions are not set up")

// Option sets an optional part of a Service.
type Option func(*Service)

// WithSuggestions sets the queue that starts suggestion runs, the model that
// writes them and the log. Without it nothing is suggested.
func WithSuggestions(queue Queue, completer Completer, log *slog.Logger) Option {
	return func(s *Service) { s.queue, s.llm, s.log = queue, completer, log }
}

// LearnedItem asks for a suggestion run about something the owner finished
// learning, inside tx, the transaction that also marks the event as seen. The
// run is unique per item, so a redelivered event adds none.
func (s *Service) LearnedItem(ctx context.Context, tx pgx.Tx, owner uuid.UUID, item domain.LearnedItem) error {
	if s.queue == nil {
		return nil
	}
	return s.queue.EnqueueSuggest(ctx, tx, SuggestInput{OwnerID: owner, Learned: &item})
}

type modelSuggestion struct {
	Target   string `json:"target"`
	Section  string `json:"section"`
	After    string `json:"after"`
	Reason   string `json:"reason"`
	Evidence []struct {
		Label string `json:"label"`
		URL   string `json:"url"`
	} `json:"evidence"`
}

// Suggest asks the model for edits grounded in what is new on GitHub and what
// the owner finished learning, and stores the ones that are valid and cite
// links from those facts. It emits profile.suggestion_ready for each. It returns
// how many it stored.
func (s *Service) Suggest(ctx context.Context, in SuggestInput) (int, error) {
	if s.llm == nil || s.pool == nil {
		return 0, ErrSuggestOff
	}
	repo := store.New(s.pool)
	changes, err := s.changesFor(ctx, repo, in)
	if err != nil {
		return 0, err
	}
	if changes.Empty() && in.Learned == nil {
		return 0, nil
	}
	facts, allowed := factsOf(changes, in.Learned)
	res, err := s.llm.Complete(ctx, featureSuggest, llm.Request{
		System:   suggestSystem,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: facts}},
	})
	if err != nil {
		return 0, fmt.Errorf("ask the model: %w", err)
	}
	proposed, err := parseAnswer(res.Text)
	if err != nil {
		return 0, err
	}
	return s.store(ctx, in.OwnerID, proposed, allowed)
}

// changesFor works out what is new: the latest snapshot against the one before.
// A run started by a snapshot looks at that one, which is the latest unless
// another has arrived since.
func (s *Service) changesFor(ctx context.Context, repo *store.Repo, in SuggestInput) (domain.Changes, error) {
	rows, err := repo.LatestSnapshots(ctx, in.OwnerID, 2)
	if err != nil {
		return domain.Changes{}, err
	}
	if len(rows) == 0 {
		return domain.Changes{}, nil
	}
	if in.SnapshotID != nil && rows[0].ID != *in.SnapshotID && len(rows) == 2 && rows[1].ID == *in.SnapshotID {
		return domain.Changes{}, nil // a newer snapshot has its own run
	}
	cur, err := snapshotOf(rows[0])
	if err != nil {
		return domain.Changes{}, err
	}
	if in.SnapshotID == nil && in.Learned != nil {
		// A learned item reads GitHub as it stands, not as it changed.
		return domain.Changes{}, nil
	}
	var prev *domain.Snapshot
	if len(rows) == 2 {
		p, err := snapshotOf(rows[1])
		if err != nil {
			return domain.Changes{}, err
		}
		prev = &p
	}
	return domain.Diff(prev, cur), nil
}

func snapshotOf(row db.GithubSnapshot) (domain.Snapshot, error) {
	var snap domain.Snapshot
	if err := json.Unmarshal(row.Repos, &snap.Repos); err != nil {
		return snap, fmt.Errorf("read repos of snapshot %s: %w", row.ID, err)
	}
	if err := json.Unmarshal(row.Contributions, &snap.Contributions); err != nil {
		return snap, fmt.Errorf("read contributions of snapshot %s: %w", row.ID, err)
	}
	return snap, nil
}

// factsOf writes the facts for the prompt and returns the links that may be
// cited as evidence, by URL.
func factsOf(c domain.Changes, learned *domain.LearnedItem) (string, map[string]bool) {
	allowed := map[string]bool{}
	var b strings.Builder
	if len(c.Repos) > 0 {
		b.WriteString("New repositories:\n")
		for _, r := range c.Repos {
			allowed[r.URL] = true
			fmt.Fprintf(&b, "- %s (%s, %d stars, last push %s) %s\n", r.Name, orDash(r.Language), r.Stars, domain.Month(r.PushedAt), r.URL)
		}
	}
	if len(c.PullRequests) > 0 {
		b.WriteString("Merged pull requests:\n")
		for _, p := range c.PullRequests {
			allowed[p.URL] = true
			fmt.Fprintf(&b, "- %s in %s (merged %s) %s\n", p.Title, p.Repo, domain.Month(p.MergedAt), p.URL)
		}
	}
	if learned != nil {
		b.WriteString("Finished learning:\n")
		fmt.Fprintf(&b, "- %s (%s)", learned.Title, learned.Kind)
		if learned.URL != "" {
			allowed[learned.URL] = true
			fmt.Fprintf(&b, " %s", learned.URL)
		}
		b.WriteString("\n")
	}
	return b.String(), allowed
}

func orDash(s string) string {
	if s == "" {
		return "no main language"
	}
	return s
}

// parseAnswer reads the model's JSON, tolerating text around it.
func parseAnswer(text string) ([]modelSuggestion, error) {
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end < start {
		return nil, ErrBadModelAnswer
	}
	var answer struct {
		Suggestions []modelSuggestion `json:"suggestions"`
	}
	if err := json.Unmarshal([]byte(text[start:end+1]), &answer); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrBadModelAnswer, err)
	}
	return answer.Suggestions, nil
}

// store validates what the model proposed, drops what cites nothing real or
// repeats a known edit, and writes the rest with their events.
func (s *Service) store(ctx context.Context, owner uuid.UUID, proposed []modelSuggestion, allowed map[string]bool) (int, error) {
	repo := store.New(s.pool)
	known, _, err := repo.ListSuggestions(ctx, owner, store.SuggestionFilter{}, store.Page{Size: knownWindow})
	if err != nil {
		return 0, err
	}
	seen := map[string]bool{}
	for _, k := range known {
		seen[dedupeKey(k.Target, k.Section, k.After)] = true
	}

	var valid []domain.Suggestion
	for _, p := range proposed[:min(len(proposed), maxSuggestions)] {
		sug, ok := s.accept(p, allowed)
		key := dedupeKey(string(sug.Target), string(sug.Section), sug.After)
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		valid = append(valid, sug)
	}
	if len(valid) == 0 {
		return 0, nil
	}
	err = postgres.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		repo := store.New(tx)
		for _, sug := range valid {
			row, err := repo.InsertSuggestion(ctx, store.NewSuggestion{
				ID: store.NewID(), OwnerID: owner, Suggestion: sug, CreatedAt: s.now().UTC(),
			})
			if err != nil {
				return err
			}
			if _, err := outbox.Write(ctx, tx, eventSource, eventSuggestionReady, row.ID.String(), &katanav1.ProfileSuggestionReady{
				SuggestionId: row.ID.String(), Target: wire.TargetToProto(sug.Target), OwnerId: owner.String(),
			}); err != nil {
				return fmt.Errorf("write %s event: %w", eventSuggestionReady, err)
			}
		}
		return nil
	})
	if err != nil {
		return 0, fmt.Errorf("store suggestions: %w", err)
	}
	return len(valid), nil
}

// accept turns a proposal into a suggestion, keeping only evidence the facts
// contained. It refuses a proposal left with none, since a suggestion nobody
// can check is not worth showing.
func (s *Service) accept(p modelSuggestion, allowed map[string]bool) (domain.Suggestion, bool) {
	sug := domain.Suggestion{
		Target: domain.Target(p.Target), Section: domain.Section(p.Section), After: p.After, Reason: p.Reason,
	}
	for _, e := range p.Evidence {
		if allowed[e.URL] {
			sug.Evidence = append(sug.Evidence, domain.Evidence{Label: e.Label, URL: e.URL})
		}
	}
	if len(sug.Evidence) == 0 {
		return sug, false
	}
	clean, err := sug.Validate()
	if err != nil {
		if s.log != nil {
			s.log.Warn("model suggestion dropped", slog.Any("reason", err))
		}
		return sug, false
	}
	return clean, true
}

func dedupeKey(target, section, after string) string {
	return target + "\x00" + section + "\x00" + strings.TrimSpace(after)
}
