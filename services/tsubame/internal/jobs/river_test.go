package jobs_test

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	"github.com/0xHoaxen/shogun/pkg/bus/relay"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/app"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/envelope"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/jobs"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail/mailtest"
	"github.com/0xHoaxen/shogun/services/tsubame/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

type nopBus struct{}

func (nopBus) Deliver(context.Context, string, *eventsv1.Envelope) error { return nil }

func TestRiverRunsTheScheduledSyncOnStartAndStoresTheMail(t *testing.T) {
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "tsubame")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if err := relay.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	keys, err := envelope.NewKeyring("k1", map[string][]byte{"k1": make([]byte, envelope.KeySize)})
	if err != nil {
		t.Fatal(err)
	}
	accounts := app.NewAccounts(pool, keys)
	if _, err := accounts.Connect(ctx, uuid.New(), "gmail", "me@example.com", "1//token"); err != nil {
		t.Fatal(err)
	}
	fake := &mailtest.Fake{Address: "me@example.com", Messages: []mail.Message{
		{ID: "m1", Direction: mail.Inbound, From: "hr@lumen.example", ReceivedAt: time.Now()},
		{ID: "m2", Direction: mail.Inbound, From: "hr@lumen.example", ReceivedAt: time.Now()},
	}}
	log := slog.New(slog.DiscardHandler)
	setup := jobs.NewSetup(app.NewSyncer(pool, accounts, fake, nil, log, nil), nil, nopReconciler{}, log)
	r, err := relay.New(relay.Config{
		Pool: pool, Bus: nopBus{}, Logger: log, FetchPollInterval: 100 * time.Millisecond,
		Workers: setup.Workers, Queues: setup.Queues, PeriodicJobs: setup.PeriodicJobs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = r.Stop(stopCtx)
	})

	deadline := time.Now().Add(30 * time.Second)
	for {
		var n int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM messages`).Scan(&n); err != nil {
			t.Fatal(err)
		}
		if n == 2 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("got %d messages, want 2: the scheduled sync did not run", n)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

type stubLinker struct{}

func (stubLinker) FindLinks(context.Context, string, []string) (app.Links, error) {
	return app.Links{}, nil
}

type noModel struct{}

func (noModel) Complete(context.Context, string, llm.Request) (llm.Response, error) {
	return llm.Response{}, errors.New("the model must not be called for a sure rule")
}

func TestASyncedMessageIsClassifiedThroughRiver(t *testing.T) {
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "tsubame")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if err := relay.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	keys, _ := envelope.NewKeyring("k1", map[string][]byte{"k1": make([]byte, envelope.KeySize)})
	accounts := app.NewAccounts(pool, keys)
	acc, err := accounts.Connect(ctx, uuid.New(), "gmail", "me@example.com", "1//token")
	if err != nil {
		t.Fatal(err)
	}
	fake := &mailtest.Fake{Address: "me@example.com", Messages: []mail.Message{{
		ID: "m1", ThreadID: "t1", Direction: mail.Inbound, From: "hr@lumen.example",
		Subject: "Thank you for applying", ReceivedAt: time.Now(),
	}}}
	queue, err := jobs.NewRiverQueue(pool)
	if err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.DiscardHandler)
	syncer := app.NewSyncer(pool, accounts, fake, queue, log, nil)
	setup := jobs.NewSetup(syncer, app.NewClassifier(pool, noModel{}, stubLinker{}), nopReconciler{}, log)
	r, err := relay.New(relay.Config{
		Pool: pool, Bus: nopBus{}, Logger: log, FetchPollInterval: 100 * time.Millisecond,
		Workers: setup.Workers, Queues: setup.Queues,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = r.Stop(stopCtx)
	})

	if _, err := syncer.SyncAccount(ctx, acc); err != nil {
		t.Fatalf("sync: %v", err)
	}

	deadline := time.Now().Add(30 * time.Second)
	for {
		var class *string
		if err := pool.QueryRow(ctx, `SELECT classification FROM messages`).Scan(&class); err != nil {
			t.Fatal(err)
		}
		if class != nil {
			var events int
			_ = pool.QueryRow(ctx, `SELECT count(*) FROM outbox WHERE type = 'mail.classified'`).Scan(&events)
			if *class != "application_confirmation" || events != 1 {
				t.Fatalf("class %q, %d events", *class, events)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the synced message was never classified")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

type nopReconciler struct{}

func (nopReconciler) Reconcile(context.Context) (app.ReconcileResult, error) {
	return app.ReconcileResult{}, nil
}
