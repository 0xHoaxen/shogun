package jobs_test

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	"github.com/0xHoaxen/shogun/pkg/bus/relay"
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
	setup := jobs.NewSetup(app.NewSyncer(pool, accounts, fake, nil, log, nil), log)
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
