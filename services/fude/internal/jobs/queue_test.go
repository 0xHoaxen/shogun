package jobs_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/bus/relay"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
	"github.com/0xHoaxen/shogun/services/fude/internal/jobs"
	"github.com/0xHoaxen/shogun/services/fude/migrations"
)

const waitFor = 30 * time.Second

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

type nopBus struct{}

func (nopBus) Deliver(context.Context, string, *eventsv1.Envelope) error { return nil }

type scriptedLLM struct{ err error }

func (s scriptedLLM) Complete(context.Context, string, llm.Request) (llm.Response, error) {
	return llm.Response{Text: "Subject: Hi\n\nHello there.", Model: "claude-test"}, s.err
}

type constEmbedder struct{ vec []float32 }

func (c constEmbedder) Embed(context.Context, string) ([]float32, error) { return c.vec, nil }

type noTarget struct{}

func (noTarget) Describe(context.Context, uuid.UUID, domain.TargetType, uuid.UUID) (app.TargetContext, error) {
	return app.TargetContext{}, nil
}

// stack is a fude service whose drafts are written by a running River client.
type stack struct {
	pool *pgxpool.Pool
	svc  *app.Service
	ctx  context.Context
}

func newStack(t *testing.T, completer app.Completer) *stack {
	return newStackWith(t, completer, nil)
}

// newStackWith is newStack with an embedder; a nil one leaves embeddings off.
func newStackWith(t *testing.T, completer app.Completer, embedder app.Embedder) *stack {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "fude")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := relay.Migrate(ctx, pool); err != nil {
		t.Fatalf("relay migrate: %v", err)
	}
	log := slog.New(slog.DiscardHandler)
	var sampleEmbedder jobs.SampleEmbedder
	if embedder != nil {
		sampleEmbedder = app.NewVoiceEmbedder(pool, embedder)
	}
	setup := jobs.NewSetup(app.NewGenerator(pool, completer, noTarget{}, log, nil), sampleEmbedder, log)
	r, err := relay.New(relay.Config{
		Pool: pool, Bus: nopBus{}, Logger: log, FetchPollInterval: 100 * time.Millisecond,
		Workers: setup.Workers, Queues: setup.Queues,
	})
	if err != nil {
		t.Fatalf("relay: %v", err)
	}
	if err := r.Start(ctx); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = r.Stop(stopCtx)
	})
	queue, err := jobs.NewRiverQueue(pool, embedder != nil)
	if err != nil {
		t.Fatalf("queue: %v", err)
	}
	owner := uuid.NewString()
	return &stack{
		pool: pool, svc: app.NewService(pool, queue, nil),
		ctx: authz.WithIdentity(ctx, authz.Identity{OwnerID: owner, RequestID: "test"}),
	}
}

func (s *stack) generate(t *testing.T, key string) string {
	t.Helper()
	d, err := s.svc.GenerateDraft(s.ctx, app.GenerateDraftInput{
		Kind: domain.KindPost, TargetType: domain.TargetNone, Channel: domain.ChannelLinkedIn, IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	return d.ID.String()
}

func (s *stack) state(t *testing.T, id string) (string, int32) {
	t.Helper()
	var state string
	var version int32
	if err := s.pool.QueryRow(s.ctx, `SELECT state, current_version FROM drafts WHERE id = $1`, id).Scan(&state, &version); err != nil {
		t.Fatalf("read draft: %v", err)
	}
	return state, version
}

func TestGenerateDraftBecomesPendingThroughRiver(t *testing.T) {
	s := newStack(t, scriptedLLM{})

	id := s.generate(t, "")

	deadline := time.Now().Add(waitFor)
	for {
		state, version := s.state(t, id)
		if state == "pending" && version == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("draft stayed %s v%d", state, version)
		}
		time.Sleep(50 * time.Millisecond)
	}
	var ready int
	if err := s.pool.QueryRow(s.ctx, `SELECT count(*) FROM outbox WHERE type = 'draft.ready'`).Scan(&ready); err != nil || ready != 1 {
		t.Fatalf("draft.ready rows %d, err %v", ready, err)
	}
}

func TestBudgetDenialSnoozesTheJobInsteadOfFailingTheDraft(t *testing.T) {
	resets := time.Now().Add(6 * time.Hour)
	s := newStack(t, scriptedLLM{err: &llm.BudgetError{ResetsAt: resets}})

	id := s.generate(t, "")

	deadline := time.Now().Add(waitFor)
	for {
		var scheduledAt time.Time
		var state string
		err := s.pool.QueryRow(s.ctx, `SELECT state, scheduled_at FROM river_job WHERE kind = 'generate_draft'`).Scan(&state, &scheduledAt)
		if err == nil && state == "scheduled" {
			if scheduledAt.Before(resets.Add(-time.Minute)) || scheduledAt.After(resets.Add(time.Minute)) {
				t.Fatalf("job scheduled for %s, want about %s", scheduledAt, resets)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("job never snoozed, last state %q err %v", state, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if state, _ := s.state(t, id); state != "generating" {
		t.Fatalf("draft is %s, want still generating", state)
	}
	var failed int
	if err := s.pool.QueryRow(s.ctx, `SELECT count(*) FROM outbox WHERE type = 'draft.failed'`).Scan(&failed); err != nil || failed != 0 {
		t.Fatalf("draft.failed rows %d, err %v", failed, err)
	}
}

func TestQueueingTheSameVersionTwiceMakesOneJob(t *testing.T) {
	s := newStack(t, scriptedLLM{err: errors.New("never finishes")})
	queue, err := jobs.NewRiverQueue(s.pool, false)
	if err != nil {
		t.Fatal(err)
	}
	args := app.GenerateArgs{OwnerID: uuid.New(), DraftID: uuid.New(), Version: 3}

	for range 2 {
		tx, err := s.pool.Begin(s.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := queue.EnqueueGenerate(s.ctx, tx, args); err != nil {
			t.Fatalf("enqueue: %v", err)
		}
		if err := tx.Commit(s.ctx); err != nil {
			t.Fatal(err)
		}
	}

	var n int
	if err := s.pool.QueryRow(s.ctx, `SELECT count(*) FROM river_job WHERE kind = 'generate_draft' AND args->>'draft_id' = $1`, args.DraftID.String()).Scan(&n); err != nil || n != 1 {
		t.Fatalf("got %d jobs, err %v; want 1", n, err)
	}
}

func TestAddVoiceSampleIsEmbeddedThroughRiverWhenAnEmbedderIsSet(t *testing.T) {
	vec := make([]float32, app.EmbeddingDimensions)
	vec[0] = 1
	s := newStackWith(t, scriptedLLM{}, constEmbedder{vec: vec})

	sample, err := s.svc.AddVoiceSample(s.ctx, domain.ChannelEmail, "my writing")
	if err != nil {
		t.Fatalf("add sample: %v", err)
	}

	deadline := time.Now().Add(waitFor)
	for {
		var embedded bool
		if err := s.pool.QueryRow(s.ctx, `SELECT embedding IS NOT NULL FROM voice_samples WHERE id = $1`, sample.ID).Scan(&embedded); err != nil {
			t.Fatal(err)
		}
		if embedded {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("sample was never embedded")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestAddVoiceSampleQueuesNoEmbedJobWhenEmbeddingsAreOff(t *testing.T) {
	s := newStack(t, scriptedLLM{})

	if _, err := s.svc.AddVoiceSample(s.ctx, domain.ChannelEmail, "my writing"); err != nil {
		t.Fatalf("add sample: %v", err)
	}

	var n int
	if err := s.pool.QueryRow(s.ctx, `SELECT count(*) FROM river_job WHERE kind = 'embed_voice_sample'`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("got %d embed jobs, err %v; want none", n, err)
	}
}
