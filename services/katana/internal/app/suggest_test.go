package app_test

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	katanav1 "github.com/0xHoaxen/shogun/gen/go/shogun/katana/v1"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/katana/internal/app"
	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
	"github.com/0xHoaxen/shogun/services/katana/internal/store"
)

const (
	repoBURL = "https://github.com/o/b"
	prURL    = "https://github.com/x/y/pull/2"
	itemURL  = "https://example.com/course"
)

// fakeLLM answers every call with text, or err, and records what it was asked.
type fakeLLM struct {
	mu       sync.Mutex
	text     string
	err      error
	features []string
	prompts  []string
	systems  []string
}

func (f *fakeLLM) Complete(_ context.Context, feature string, req llm.Request) (llm.Response, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.features = append(f.features, feature)
	f.systems = append(f.systems, req.System)
	f.prompts = append(f.prompts, req.Messages[0].Content)
	return llm.Response{Text: f.text}, f.err
}

// fakeQueue records the runs it is asked to enqueue, and can fail.
type fakeQueue struct {
	runs []app.SuggestInput
	err  error
}

func (q *fakeQueue) EnqueueSuggest(_ context.Context, _ pgx.Tx, in app.SuggestInput) error {
	if q.err != nil {
		return q.err
	}
	q.runs = append(q.runs, in)
	return nil
}

func answer(items ...string) string {
	return `Here you go: {"suggestions":[` + strings.Join(items, ",") + `]} done.`
}

func item(target, section, after, evidenceURL string) string {
	return `{"target":"` + target + `","section":"` + section + `","after":"` + after + `","reason":"seen in the facts",` +
		`"evidence":[{"label":"link","url":"` + evidenceURL + `"}]}`
}

func seed(t *testing.T, pool *pgxpool.Pool, owner uuid.UUID, repos, prs string, i int) uuid.UUID {
	t.Helper()
	row, err := store.New(pool).InsertSnapshot(context.Background(), store.NewSnapshot{
		ID: store.NewID(), OwnerID: owner, TakenAt: testNow.Add(time.Duration(i) * time.Hour),
		Repos: []byte(repos), Contributions: []byte(`{"merged_prs":` + prs + `}`),
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	return row.ID
}

const (
	oldRepos = `[{"name":"a","url":"https://github.com/o/a","language":"Go","stars":1,"pushed_at":"2026-09-01T00:00:00Z"}]`
	newRepos = `[{"name":"a","url":"https://github.com/o/a","language":"Go","stars":1,"pushed_at":"2026-09-01T00:00:00Z"},
	             {"name":"b","url":"` + repoBURL + `","language":"Rust","stars":4,"pushed_at":"2026-10-05T00:00:00Z"}]`
	newPRs = `[{"repo":"x/y","title":"Add worker pool","url":"` + prURL + `","merged_at":"2026-10-04T00:00:00Z"}]`
)

func newSuggester(t *testing.T, model *fakeLLM) (*app.Service, *pgxpool.Pool) {
	t.Helper()
	pool := newPool(t)
	svc := app.NewService(pool, nil, func() time.Time { return testNow },
		app.WithSuggestions(&fakeQueue{}, model, slog.New(slog.DiscardHandler)))
	return svc, pool
}

func outboxPayloads(t *testing.T, pool *pgxpool.Pool) []*katanav1.ProfileSuggestionReady {
	t.Helper()
	rows, err := pool.Query(context.Background(), `SELECT payload FROM outbox WHERE type = 'profile.suggestion_ready'`)
	if err != nil {
		t.Fatalf("query outbox: %v", err)
	}
	defer rows.Close()
	var out []*katanav1.ProfileSuggestionReady
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var env eventsv1.Envelope
		var p katanav1.ProfileSuggestionReady
		if err := proto.Unmarshal(raw, &env); err != nil || env.GetPayload().UnmarshalTo(&p) != nil {
			t.Fatalf("decode: %v", err)
		}
		out = append(out, &p)
	}
	return out
}

func TestSuggestStoresGroundedSuggestionsAndAnnouncesEach(t *testing.T) {
	model := &fakeLLM{text: answer(
		item("resume", "projects", "Built a Rust tool", repoBURL),
		item("linkedin", "skills", "Rust", "https://invented.example/proof"),    // evidence not in the facts
		item("resume", "hobbies", "Climbing", repoBURL),                         // unknown section
		item("cv", "skills", "x", prURL),                                        // unknown target
		item("linkedin", "experience", "Shipped a worker pool in Go", prURL+""), // good
	)}
	svc, pool := newSuggester(t, model)
	owner := uuid.New()
	seed(t, pool, owner, oldRepos, `[]`, 0)
	cur := seed(t, pool, owner, newRepos, newPRs, 1)

	n, err := svc.Suggest(context.Background(), app.SuggestInput{OwnerID: owner, SnapshotID: &cur})

	if err != nil || n != 2 {
		t.Fatalf("stored %d, err %v; want the two grounded suggestions", n, err)
	}
	rows, _, _ := store.New(pool).ListSuggestions(context.Background(), owner, store.SuggestionFilter{}, store.Page{})
	if len(rows) != 2 || rows[0].State != "open" || rows[0].Before != nil {
		t.Fatalf("rows = %+v", rows)
	}
	events := outboxPayloads(t, pool)
	if len(events) != 2 || events[0].GetOwnerId() != owner.String() || events[0].GetSuggestionId() == "" {
		t.Fatalf("events = %v, want one per stored suggestion naming the owner", events)
	}
	if len(model.features) != 1 || model.features[0] != "katana.suggest" {
		t.Fatalf("features = %v", model.features)
	}
	prompt := model.prompts[0]
	for _, want := range []string{"New repositories", repoBURL, "Merged pull requests", "Add worker pool", prURL} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "https://github.com/o/a") {
		t.Errorf("prompt lists a repository that was already known:\n%s", prompt)
	}
}

func TestSuggestDoesNotCallTheModelWhenNothingIsNew(t *testing.T) {
	model := &fakeLLM{text: answer()}
	svc, pool := newSuggester(t, model)
	owner := uuid.New()
	seed(t, pool, owner, newRepos, newPRs, 0)
	cur := seed(t, pool, owner, newRepos, newPRs, 1)

	n, err := svc.Suggest(context.Background(), app.SuggestInput{OwnerID: owner, SnapshotID: &cur})
	noSnapshots, noErr := svc.Suggest(context.Background(), app.SuggestInput{OwnerID: uuid.New()})

	if err != nil || n != 0 || noErr != nil || noSnapshots != 0 || len(model.features) != 0 {
		t.Fatalf("n %d err %v, none %d %v, model calls %d; want no call", n, err, noSnapshots, noErr, len(model.features))
	}
}

func TestSuggestIgnoresARunForASnapshotThatHasBeenSuperseded(t *testing.T) {
	model := &fakeLLM{text: answer()}
	svc, pool := newSuggester(t, model)
	owner := uuid.New()
	older := seed(t, pool, owner, newRepos, newPRs, 0)
	seed(t, pool, owner, newRepos, newPRs, 1)

	_, err := svc.Suggest(context.Background(), app.SuggestInput{OwnerID: owner, SnapshotID: &older})

	if err != nil || len(model.features) != 0 {
		t.Fatalf("err %v, model calls %d; want the newer snapshot's own run to do the work", err, len(model.features))
	}
}

func TestSuggestFromAFinishedItemCitesTheItemLink(t *testing.T) {
	model := &fakeLLM{text: answer(item("resume", "skills", "Go concurrency", itemURL), item("resume", "skills", "No link", "https://nope.example"))}
	svc, pool := newSuggester(t, model)
	owner := uuid.New()

	n, err := svc.Suggest(context.Background(), app.SuggestInput{
		OwnerID: owner, Learned: &domain.LearnedItem{ItemID: uuid.NewString(), Title: "Go concurrency course", Kind: "course", URL: itemURL},
	})

	if err != nil || n != 1 {
		t.Fatalf("stored %d, err %v", n, err)
	}
	if !strings.Contains(model.prompts[0], "Finished learning") || !strings.Contains(model.prompts[0], "Go concurrency course") || !strings.Contains(model.prompts[0], itemURL) {
		t.Fatalf("prompt = %s", model.prompts[0])
	}
	rows, _, _ := store.New(pool).ListSuggestions(context.Background(), owner, store.SuggestionFilter{}, store.Page{})
	if len(rows) != 1 || !strings.Contains(string(rows[0].Evidence), itemURL) {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestSuggestFromAnItemWithoutALinkStoresNothingCitable(t *testing.T) {
	model := &fakeLLM{text: answer(item("resume", "skills", "Go", "https://made-up.example"))}
	svc, _ := newSuggester(t, model)

	n, err := svc.Suggest(context.Background(), app.SuggestInput{
		OwnerID: uuid.New(), Learned: &domain.LearnedItem{ItemID: uuid.NewString(), Title: "Go course", Kind: "course"},
	})

	if err != nil || n != 0 {
		t.Fatalf("stored %d, err %v; want nothing without checkable evidence", n, err)
	}
}

func TestSuggestDoesNotRepeatAKnownEdit(t *testing.T) {
	model := &fakeLLM{text: answer(item("resume", "projects", "Built a Rust tool", repoBURL))}
	svc, pool := newSuggester(t, model)
	owner := uuid.New()
	seed(t, pool, owner, oldRepos, `[]`, 0)
	cur := seed(t, pool, owner, newRepos, newPRs, 1)
	in := app.SuggestInput{OwnerID: owner, SnapshotID: &cur}

	first, _ := svc.Suggest(context.Background(), in)
	second, err := svc.Suggest(context.Background(), in)

	rows, _, _ := store.New(pool).ListSuggestions(context.Background(), owner, store.SuggestionFilter{}, store.Page{})
	if err != nil || first != 1 || second != 0 || len(rows) != 1 || len(outboxPayloads(t, pool)) != 1 {
		t.Fatalf("first %d, second %d, err %v, rows %d", first, second, err, len(rows))
	}
}

func TestSuggestStoresAtMostFive(t *testing.T) {
	var items []string
	for _, section := range []string{"headline", "about", "experience", "skills", "projects"} {
		items = append(items, item("resume", section, "text "+section, repoBURL), item("linkedin", section, "text "+section, repoBURL))
	}
	svc, pool := newSuggester(t, &fakeLLM{text: answer(items...)})
	owner := uuid.New()
	cur := seed(t, pool, owner, newRepos, newPRs, 0)

	n, err := svc.Suggest(context.Background(), app.SuggestInput{OwnerID: owner, SnapshotID: &cur})

	if err != nil || n != 5 {
		t.Fatalf("stored %d, err %v, want 5", n, err)
	}
}

func TestSuggestFailsAndStoresNothingOnAnUnreadableAnswerOrAModelError(t *testing.T) {
	tests := []struct {
		name  string
		model *fakeLLM
		want  error
	}{
		{"prose", &fakeLLM{text: "I could not think of anything."}, app.ErrBadModelAnswer},
		{"broken json", &fakeLLM{text: `{"suggestions":[{`}, app.ErrBadModelAnswer},
		{"budget", &fakeLLM{err: &llm.BudgetError{ResetsAt: testNow}}, nil},
		{"model down", &fakeLLM{err: errors.New("boom")}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, pool := newSuggester(t, tt.model)
			owner := uuid.New()
			cur := seed(t, pool, owner, newRepos, newPRs, 0)

			n, err := svc.Suggest(context.Background(), app.SuggestInput{OwnerID: owner, SnapshotID: &cur})

			rows, _, _ := store.New(pool).ListSuggestions(context.Background(), owner, store.SuggestionFilter{}, store.Page{})
			if err == nil || n != 0 || len(rows) != 0 || (tt.want != nil && !errors.Is(err, tt.want)) {
				t.Fatalf("n %d, err %v, rows %d", n, err, len(rows))
			}
			var budget *llm.BudgetError
			if tt.name == "budget" && !errors.As(err, &budget) {
				t.Fatalf("err = %v, want the budget error kept so the job can snooze", err)
			}
		})
	}
}

func TestSuggestIsOffWithoutAModel(t *testing.T) {
	_, err := app.NewService(newPool(t), nil, nil).Suggest(context.Background(), app.SuggestInput{OwnerID: uuid.New()})

	if !errors.Is(err, app.ErrSuggestOff) {
		t.Fatalf("err = %v", err)
	}
}

func TestSyncStartsOneRunForTheSnapshotInTheSameTransaction(t *testing.T) {
	pool, owner := newPool(t), uuid.New()
	queue := &fakeQueue{}
	gh := &fakeGitHub{snap: snapshot(`W/"a"`)}
	svc := app.NewService(pool, gh, func() time.Time { return testNow }, app.WithSuggestions(queue, &fakeLLM{}, slog.New(slog.DiscardHandler)))

	res, err := svc.SyncGitHub(asOwner(owner))
	again, againErr := svc.SyncGitHub(asOwner(owner)) // not modified

	if err != nil || againErr != nil || again.Changed {
		t.Fatalf("errs %v %v, again %+v", err, againErr, again)
	}
	if len(queue.runs) != 1 || queue.runs[0].OwnerID != owner || queue.runs[0].SnapshotID == nil || *queue.runs[0].SnapshotID != res.SnapshotID {
		t.Fatalf("runs = %+v, want one for snapshot %s", queue.runs, res.SnapshotID)
	}
}

func TestSyncStoresNoSnapshotWhenTheRunCannotBeQueued(t *testing.T) {
	pool := newPool(t)
	queue := &fakeQueue{err: errors.New("queue down")}
	svc := app.NewService(pool, &fakeGitHub{snap: snapshot("e")}, nil, app.WithSuggestions(queue, &fakeLLM{}, slog.New(slog.DiscardHandler)))

	_, err := svc.SyncGitHub(asOwner(uuid.New()))

	if err == nil || count(t, pool) != 0 {
		t.Fatalf("err %v, snapshots %d; want the snapshot rolled back with the run", err, count(t, pool))
	}
}

func TestLearnedItemQueuesARunInTheCallersTransaction(t *testing.T) {
	pool, owner := newPool(t), uuid.New()
	queue := &fakeQueue{}
	svc := app.NewService(pool, nil, nil, app.WithSuggestions(queue, &fakeLLM{}, slog.New(slog.DiscardHandler)))
	tx, err := pool.Begin(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()

	err = svc.LearnedItem(context.Background(), tx, owner, domain.LearnedItem{ItemID: "i", Title: "Go course"})

	if err != nil || len(queue.runs) != 1 || queue.runs[0].Learned == nil || queue.runs[0].Learned.Title != "Go course" || queue.runs[0].SnapshotID != nil {
		t.Fatalf("err %v, runs %+v", err, queue.runs)
	}
}
