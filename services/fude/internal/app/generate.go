package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	"github.com/0xHoaxen/shogun/pkg/hanko"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/pkg/outbox"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
	"github.com/0xHoaxen/shogun/services/fude/internal/store"
	"github.com/0xHoaxen/shogun/services/fude/internal/store/db"
	"github.com/0xHoaxen/shogun/services/fude/internal/wire"
)

// Event types and failure reasons written by generation.
const (
	eventSource      = "fude"
	eventDraftReady  = "draft.ready"
	eventDraftFailed = "draft.failed"

	// voiceSampleCount is how many of the owner's samples shape a draft.
	voiceSampleCount = 5

	ReasonBudgetExhausted     = "budget_exhausted"
	ReasonMeteringUnavailable = "metering_unavailable"
	ReasonModelRefused        = "model_refused"
	ReasonContextUnavailable  = "context_unavailable"
	ReasonEmptyDraft          = "empty_draft"
	ReasonGenerationFailed    = "generation_failed"
)

// Completer is the part of *llm.Client generation needs.
type Completer interface {
	Complete(ctx context.Context, feature string, req llm.Request) (llm.Response, error)
}

// TargetContext is what is known about the job, contact or activity a draft is
// about.
type TargetContext struct {
	// Summary is plain text for the prompt.
	Summary string
	// ContactStatus picks the outreach template; empty when not about a contact.
	ContactStatus string
}

// ContextSource describes a draft's target, from the service that owns it.
type ContextSource interface {
	Describe(ctx context.Context, owner uuid.UUID, target domain.TargetType, id uuid.UUID) (TargetContext, error)
}

// Generator writes the AI versions of drafts.
type Generator struct {
	pool    *pgxpool.Pool
	llm     Completer
	context ContextSource
	log     *slog.Logger
	now     func() time.Time
}

// NewGenerator returns a Generator. A nil now means time.Now.
func NewGenerator(pool *pgxpool.Pool, completer Completer, source ContextSource, log *slog.Logger, now func() time.Time) *Generator {
	if now == nil {
		now = time.Now
	}
	return &Generator{pool: pool, llm: completer, context: source, log: log, now: now}
}

// Generate writes version args.Version of a draft and moves it to pending. It
// does nothing when that version already exists (a retry after success, or an
// edit that took the number) or when the draft is no longer waiting for one.
// A *llm.BudgetError means the caller should try again after ResetsAt.
func (g *Generator) Generate(ctx context.Context, args GenerateArgs) error {
	repo := store.New(g.pool)
	d, err := repo.GetDraft(ctx, args.OwnerID, args.DraftID)
	if err != nil {
		return err
	}
	if !waitingForVersion(d, args.Version) {
		return nil
	}

	in, err := g.gather(ctx, repo, d, args)
	if err != nil {
		return err
	}
	resp, err := g.llm.Complete(ctx, featureFor(domain.Kind(d.Kind)), buildPrompt(in))
	if err != nil {
		return fmt.Errorf("complete: %w", err)
	}
	subject, body, err := parseOutput(domain.Channel(d.Channel), resp.Text)
	if err != nil {
		return err
	}
	return g.save(ctx, args, d, subject, body, resp.Model, in)
}

// waitingForVersion reports whether a draft still needs version v written: it
// is generating or pending, and has not got v yet.
func waitingForVersion(d db.Draft, v int32) bool {
	state := domain.DraftState(d.State)
	return (state == domain.DraftGenerating || state == domain.DraftPending) && d.CurrentVersion < v
}

// contextError marks a failure to read the draft's target.
type contextError struct{ err error }

func (e *contextError) Error() string { return "describe target: " + e.err.Error() }
func (e *contextError) Unwrap() error { return e.err }

func (g *Generator) gather(ctx context.Context, repo *store.Repo, d db.Draft, args GenerateArgs) (promptInput, error) {
	kind, channel := domain.Kind(d.Kind), domain.Channel(d.Channel)
	var target TargetContext
	if d.TargetID != nil && domain.TargetType(d.TargetType).NeedsTargetID() {
		var err error
		if target, err = g.context.Describe(ctx, args.OwnerID, domain.TargetType(d.TargetType), *d.TargetID); err != nil {
			return promptInput{}, &contextError{err: err}
		}
	}
	samples, err := repo.ListRecentVoiceSamples(ctx, args.OwnerID, d.Channel, voiceSampleCount)
	if err != nil {
		return promptInput{}, err
	}
	voice := make([]string, len(samples))
	for i, s := range samples {
		voice[i] = s.Text
	}
	instructions, err := g.instructions(ctx, repo, args.OwnerID, kind, channel, target.ContactStatus)
	if err != nil {
		return promptInput{}, err
	}
	return promptInput{
		Kind: kind, Channel: channel, Instructions: instructions, Target: target.Summary,
		ExtraContext: args.ExtraContext, Voice: voice,
	}, nil
}

// instructions returns the owner's template for the draft, or the default.
func (g *Generator) instructions(ctx context.Context, repo *store.Repo, owner uuid.UUID, kind domain.Kind, channel domain.Channel, contactStatus string) (string, error) {
	var status *string
	if kind == domain.KindOutreach && contactStatus != "" {
		status = &contactStatus
	}
	t, err := repo.GetTemplate(ctx, owner, string(kind), string(channel), status)
	if errors.Is(err, store.ErrNotFound) {
		return defaultInstructions[kind], nil
	}
	if err != nil {
		return "", err
	}
	return t.Instructions, nil
}

// save writes the version, moves the draft and records draft.ready in one
// transaction.
func (g *Generator) save(ctx context.Context, args GenerateArgs, d db.Draft, subject, body, model string, in promptInput) error {
	promptContext, err := json.Marshal(map[string]any{
		"feature": featureFor(in.Kind), "voice_samples": len(in.Voice), "has_target": in.Target != "",
	})
	if err != nil {
		return fmt.Errorf("marshal prompt context: %w", err)
	}
	return postgres.InTx(ctx, g.pool, func(tx pgx.Tx) error {
		repo := store.New(tx)
		current := toDomain(d)
		var next domain.Draft
		var moveErr error
		if current.State == domain.DraftGenerating {
			next, moveErr = current.Generated(args.Version, g.now())
		} else {
			next, moveErr = current.NewVersion(args.Version, g.now())
		}
		if moveErr != nil {
			return moveErr
		}
		saved, err := saveState(ctx, repo, args.OwnerID, d, next, d.Version)
		if err != nil {
			return err
		}
		if _, err := repo.InsertDraftVersion(ctx, db.InsertDraftVersionParams{
			DraftID: d.ID, Version: args.Version, Subject: strPtr(subject), Body: body,
			BodySha256: hanko.BodyDigest(subject, body), ExtraContext: strPtr(args.ExtraContext),
			Model: strPtr(model), PromptContext: promptContext, CreatedBy: createdByAI,
		}); err != nil {
			return err
		}
		_, err = outbox.Write(ctx, tx, eventSource, eventDraftReady, saved.ID.String(), &fudev1.DraftReady{
			DraftId: saved.ID.String(), Kind: wire.KindToProto(domain.Kind(saved.Kind)),
			TargetType: wire.TargetTypeToProto(domain.TargetType(saved.TargetType)),
			TargetId:   targetID(saved), Version: args.Version,
		})
		return err
	})
}

func targetID(d db.Draft) string {
	if d.TargetID == nil {
		return ""
	}
	return d.TargetID.String()
}

// Fail records that generation gave up. A generating draft becomes failed; a
// pending one (a failed regenerate) keeps its current version and stays
// pending. Either way draft.failed is emitted once, with a short reason that
// never carries draft content.
func (g *Generator) Fail(ctx context.Context, args GenerateArgs, reason string) error {
	return postgres.InTx(ctx, g.pool, func(tx pgx.Tx) error {
		repo := store.New(tx)
		d, err := repo.GetDraft(ctx, args.OwnerID, args.DraftID)
		if err != nil {
			return err
		}
		if !waitingForVersion(d, args.Version) {
			return nil
		}
		if domain.DraftState(d.State) == domain.DraftGenerating {
			next, err := toDomain(d).GenerationFailed(reason, g.now())
			if err != nil {
				return err
			}
			if _, err := saveState(ctx, repo, args.OwnerID, d, next, d.Version); err != nil {
				return err
			}
		}
		_, err = outbox.Write(ctx, tx, eventSource, eventDraftFailed, d.ID.String(), &fudev1.DraftFailed{
			DraftId: d.ID.String(), Reason: reason,
		})
		return err
	})
}

// FailureReason maps a generation error to the short reason stored on the
// draft and sent in draft.failed.
func FailureReason(err error) string {
	var ce *contextError
	switch {
	case errors.Is(err, llm.ErrBudgetExhausted):
		return ReasonBudgetExhausted
	case errors.Is(err, llm.ErrMeteringUnavailable):
		return ReasonMeteringUnavailable
	case errors.Is(err, llm.ErrRefused):
		return ReasonModelRefused
	case errors.Is(err, ErrEmptyDraft):
		return ReasonEmptyDraft
	case errors.As(err, &ce):
		return ReasonContextUnavailable
	default:
		return ReasonGenerationFailed
	}
}
