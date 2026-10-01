package inbox_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	"github.com/0xHoaxen/shogun/pkg/inbox"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

func newPool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "kagami")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, postgres.OutboxInboxSQL); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return ctx, pool
}

func newEnvelope(t *testing.T) *eventsv1.Envelope {
	t.Helper()
	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("uuid: %v", err)
	}
	return &eventsv1.Envelope{Id: id.String(), Type: "job.added", Source: "kagami"}
}

func inboxCount(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM inbox`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestHandleRunsFnOnceForDuplicateEvent(t *testing.T) {
	ctx, pool := newPool(t)
	env := newEnvelope(t)
	calls := 0
	fn := func(context.Context, pgx.Tx) error { calls++; return nil }

	for range 2 {
		if err := inbox.Handle(ctx, pool, env, fn); err != nil {
			t.Fatalf("handle: %v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("fn calls = %d, want 1", calls)
	}
	if got := inboxCount(ctx, t, pool); got != 1 {
		t.Fatalf("inbox rows = %d, want 1", got)
	}
}

func TestHandleErrorRollsBackSoRetryRunsFnAgain(t *testing.T) {
	ctx, pool := newPool(t)
	env := newEnvelope(t)
	boom := errors.New("boom")
	calls := 0

	err := inbox.Handle(ctx, pool, env, func(context.Context, pgx.Tx) error { calls++; return boom })
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want boom", err)
	}
	if got := inboxCount(ctx, t, pool); got != 0 {
		t.Fatalf("inbox rows after failure = %d, want 0", got)
	}

	err = inbox.Handle(ctx, pool, env, func(context.Context, pgx.Tx) error { calls++; return nil })
	if err != nil {
		t.Fatalf("retry: %v", err)
	}
	if calls != 2 {
		t.Fatalf("fn calls = %d, want 2", calls)
	}
	if got := inboxCount(ctx, t, pool); got != 1 {
		t.Fatalf("inbox rows after retry = %d, want 1", got)
	}
}

func TestHandleRejectsInvalidInput(t *testing.T) {
	ctx, pool := newPool(t)
	noop := func(context.Context, pgx.Tx) error { return nil }

	cases := map[string]struct {
		env *eventsv1.Envelope
		fn  func(context.Context, pgx.Tx) error
	}{
		"nil envelope": {nil, noop},
		"nil handler":  {newEnvelope(t), nil},
		"bad id":       {&eventsv1.Envelope{Id: "nope"}, noop},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if err := inbox.Handle(ctx, pool, tc.env, tc.fn); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
