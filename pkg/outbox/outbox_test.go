package outbox_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/wrapperspb"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	"github.com/0xHoaxen/shogun/pkg/outbox"
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

func outboxCount(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM outbox`).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestWriteVisibleAndNotifiedOnlyAfterCommit(t *testing.T) {
	ctx, pool := newPool(t)

	listener, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer listener.Release()
	if _, err := listener.Exec(ctx, `LISTEN `+outbox.Channel); err != nil {
		t.Fatalf("listen: %v", err)
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	id, err := outbox.Write(ctx, tx, "kagami", "job.added", "job/1", wrapperspb.String("hello"))
	if err != nil {
		t.Fatalf("write: %v", err)
	}

	// Before commit: invisible to another connection and no notification.
	if got := outboxCount(ctx, t, pool); got != 0 {
		t.Fatalf("rows before commit = %d, want 0", got)
	}
	shortCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	_, err = listener.Conn().WaitForNotification(shortCtx)
	cancel()
	if err == nil {
		t.Fatal("received notification before commit")
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	if got := outboxCount(ctx, t, pool); got != 1 {
		t.Fatalf("rows after commit = %d, want 1", got)
	}
	waitCtx, cancelWait := context.WithTimeout(ctx, 5*time.Second)
	defer cancelWait()
	n, err := listener.Conn().WaitForNotification(waitCtx)
	if err != nil {
		t.Fatalf("wait for notification: %v", err)
	}
	if n.Channel != outbox.Channel || n.Payload != id.String() {
		t.Fatalf("notification = %q/%q, want %q/%s", n.Channel, n.Payload, outbox.Channel, id)
	}

	assertStoredEnvelope(ctx, t, pool, id.String())
}

func assertStoredEnvelope(ctx context.Context, t *testing.T, pool *pgxpool.Pool, id string) {
	t.Helper()
	var (
		raw         []byte
		typ, source string
		delivered   *time.Time
	)
	err := pool.QueryRow(ctx,
		`SELECT payload, type, source, delivered_at FROM outbox WHERE id = $1`, id).
		Scan(&raw, &typ, &source, &delivered)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	if typ != "job.added" || source != "kagami" || delivered != nil {
		t.Fatalf("row = %q %q delivered=%v", typ, source, delivered)
	}
	var env eventsv1.Envelope
	if err := proto.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var got wrapperspb.StringValue
	if err := env.GetPayload().UnmarshalTo(&got); err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if env.GetId() != id || env.GetSubject() != "job/1" || got.GetValue() != "hello" || env.GetOccurredAt() == nil {
		t.Fatalf("envelope = %v", &env)
	}
}

func TestWriteRolledBackLeavesNothing(t *testing.T) {
	ctx, pool := newPool(t)

	err := postgres.InTx(ctx, pool, func(tx pgx.Tx) error {
		if _, err := outbox.Write(ctx, tx, "kagami", "job.added", "", wrapperspb.String("x")); err != nil {
			return err
		}
		return context.Canceled
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if got := outboxCount(ctx, t, pool); got != 0 {
		t.Fatalf("rows = %d, want 0", got)
	}
}

func TestWriteRejectsInvalidInput(t *testing.T) {
	ctx, pool := newPool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	cases := []struct {
		name        string
		source, typ string
		payload     proto.Message
		nilTx       bool
	}{
		{name: "empty source", typ: "t", payload: wrapperspb.String("x")},
		{name: "empty type", source: "s", payload: wrapperspb.String("x")},
		{name: "nil payload", source: "s", typ: "t"},
		{name: "nil tx", source: "s", typ: "t", payload: wrapperspb.String("x"), nilTx: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			useTx := tx
			if tc.nilTx {
				useTx = nil
			}
			if _, err := outbox.Write(ctx, useTx, tc.source, tc.typ, "", tc.payload); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
