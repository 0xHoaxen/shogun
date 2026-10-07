package events_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"maps"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/sensei/internal/app"
	"github.com/0xHoaxen/shogun/services/sensei/internal/events"
	"github.com/0xHoaxen/shogun/services/sensei/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

var (
	when     = time.Date(2026, 10, 7, 8, 30, 0, 0, time.UTC)
	fallback = time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
)

type consumer struct {
	sink *bus.SinkServer
	pool *pgxpool.Pool
}

func newConsumer(t *testing.T) *consumer {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "sensei")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	log := slog.New(slog.DiscardHandler)
	sink, err := bus.NewSinkServer(pool, events.Handlers(app.NewService(pool, nil), log, func() time.Time { return fallback }), log)
	if err != nil {
		t.Fatalf("sink: %v", err)
	}
	return &consumer{sink: sink, pool: pool}
}

func (c *consumer) deliver(t *testing.T, id, typ string, payload proto.Message, at *timestamppb.Timestamp) error {
	t.Helper()
	packed, err := anypb.New(payload)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	_, err = c.sink.Deliver(context.Background(), &eventsv1.DeliverRequest{Envelope: &eventsv1.Envelope{
		Id: id, Type: typ, Source: "test", Payload: packed, OccurredAt: at,
	}})
	return err
}

type fact struct {
	owner, typ string
	dim        map[string]string
	at         time.Time
}

func (c *consumer) facts(t *testing.T) map[string]fact {
	t.Helper()
	rows, err := c.pool.Query(context.Background(), `SELECT event_id::text, owner_id::text, type, dimension, occurred_at FROM facts`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	out := map[string]fact{}
	for rows.Next() {
		var id string
		var f fact
		var raw []byte
		if err := rows.Scan(&id, &f.owner, &f.typ, &raw, &f.at); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &f.dim); err != nil {
			t.Fatal(err)
		}
		out[id] = f
	}
	return out
}

func TestEachEventTypeBecomesOneFactForTheOwner(t *testing.T) {
	owner := uuid.NewString()
	tests := []struct {
		name    string
		typ     string
		payload proto.Message
		want    map[string]string
	}{
		{"job added", "job.added", &kagamiv1.JobAdded{OwnerId: owner, JobId: "j1", Source: "LinkedIn", Title: "SECRET TITLE", Company: "Acme"}, map[string]string{"source": "linkedin", "job_id": "j1"}},
		{"job added without a source", "job.added", &kagamiv1.JobAdded{OwnerId: owner}, map[string]string{"source": "unknown", "job_id": "unknown"}},
		{"job status", "job.status_changed", &kagamiv1.JobStatusChanged{OwnerId: owner, JobId: "j1", From: kagamiv1.JobStatus_JOB_STATUS_SAVED, To: kagamiv1.JobStatus_JOB_STATUS_APPLIED}, map[string]string{"job_id": "j1", "from": "saved", "to": "applied"}},
		{"job follow-up", "job.follow_up_due", &kagamiv1.JobFollowUpDue{OwnerId: owner}, map[string]string{}},
		{"contact added", "contact.added", &kagamiv1.ContactAdded{OwnerId: owner, Status: kagamiv1.ContactStatus_CONTACT_STATUS_NOT_REACHED}, map[string]string{"status": "not_reached"}},
		{"contact status", "contact.status_changed", &kagamiv1.ContactStatusChanged{
			OwnerId: owner, From: kagamiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT, To: kagamiv1.ContactStatus_CONTACT_STATUS_REPLIED, Channel: "LinkedIn",
		}, map[string]string{"from": "reached_out", "to": "replied", "channel": "linkedin"}},
		{"contact follow-up", "contact.follow_up_due", &kagamiv1.ContactFollowUpDue{OwnerId: owner}, map[string]string{}},
		{"mail classified, linked to both", "mail.classified", &tsubamev1.MailClassified{
			OwnerId: owner, Classification: tsubamev1.MailClass_MAIL_CLASS_INTERVIEW_INVITE, JobId: "j", ContactId: "c",
		}, map[string]string{"classification": "interview_invite", "linked": "both"}},
		{"mail classified, unlinked", "mail.classified", &tsubamev1.MailClassified{OwnerId: owner, Classification: tsubamev1.MailClass_MAIL_CLASS_OTHER}, map[string]string{"classification": "other", "linked": "none"}},
		{"mail reply", "mail.reply_detected", &tsubamev1.MailReplyDetected{OwnerId: owner}, map[string]string{}},
		{"draft approved", "draft.approved", &fudev1.DraftApproved{OwnerId: owner, DraftId: "d1", Channel: fudev1.Channel_CHANNEL_LINKEDIN}, map[string]string{"channel": "linkedin", "draft_id": "d1"}},
		{"draft sent", "draft.sent", &tsubamev1.DraftSent{OwnerId: owner, DraftId: "d2"}, map[string]string{"channel": "email", "draft_id": "d2"}},
		{"cost threshold", "cost.threshold_reached", &sorobanv1.CostThresholdReached{
			OwnerId: owner, ScopeType: sorobanv1.ScopeType_SCOPE_TYPE_SERVICE, ScopeValue: "fude", Period: sorobanv1.BudgetPeriod_BUDGET_PERIOD_MONTHLY, Percent: 80,
		}, map[string]string{"scope": "service:fude", "percent": "80", "period": "monthly"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newConsumer(t)
			id := uuid.NewString()

			err := c.deliver(t, id, tt.typ, tt.payload, timestamppb.New(when))

			got := c.facts(t)
			f, ok := got[id]
			if err != nil || len(got) != 1 || !ok || f.owner != owner || f.typ != tt.typ || !f.at.Equal(when) || !maps.Equal(f.dim, tt.want) {
				t.Fatalf("err %v, facts %+v; want one %s fact for the owner with %v at %v", err, got, tt.typ, tt.want, when)
			}
		})
	}
}

func TestEveryTypeSenseiConsumesHasAHandler(t *testing.T) {
	want := map[string]bool{}
	for _, typ := range events.Types() {
		want[typ] = true
	}
	handlers := events.Handlers(app.NewService(nil, nil), slog.New(slog.DiscardHandler), nil)

	for typ := range want {
		if handlers[typ] == nil {
			t.Errorf("no handler for %s", typ)
		}
	}
	if len(handlers) != 11 || len(want) != 11 {
		t.Fatalf("handlers %d, types %d; want the eleven in the LLD", len(handlers), len(want))
	}
}

func TestAnEnvelopeWithoutATimeUsesTheClock(t *testing.T) {
	c := newConsumer(t)
	id := uuid.NewString()

	err := c.deliver(t, id, "mail.reply_detected", &tsubamev1.MailReplyDetected{OwnerId: uuid.NewString()}, nil)

	if f := c.facts(t)[id]; err != nil || !f.at.Equal(fallback) {
		t.Fatalf("err %v, fact %+v; want the fallback time", err, f)
	}
}

func TestADuplicateDeliveryStoresOneFact(t *testing.T) {
	c := newConsumer(t)
	payload := &kagamiv1.JobStatusChanged{OwnerId: uuid.NewString(), To: kagamiv1.JobStatus_JOB_STATUS_APPLIED}
	id := uuid.NewString()

	first := c.deliver(t, id, "job.status_changed", payload, timestamppb.New(when))
	again := c.deliver(t, id, "job.status_changed", payload, timestamppb.New(when))

	if first != nil || again != nil || len(c.facts(t)) != 1 {
		t.Fatalf("errs %v %v, facts %d; want one", first, again, len(c.facts(t)))
	}
}

func TestTwoEventsAboutTheSameThingAreTwoFacts(t *testing.T) {
	c := newConsumer(t)
	payload := &kagamiv1.JobStatusChanged{OwnerId: uuid.NewString(), To: kagamiv1.JobStatus_JOB_STATUS_APPLIED}

	_ = c.deliver(t, uuid.NewString(), "job.status_changed", payload, timestamppb.New(when))
	_ = c.deliver(t, uuid.NewString(), "job.status_changed", payload, timestamppb.New(when))

	if n := len(c.facts(t)); n != 2 {
		t.Fatalf("facts = %d, want one per event", n)
	}
}

func TestEventsThatCannotBeHandledAreAcknowledgedWithoutAFact(t *testing.T) {
	tests := []struct {
		name    string
		id      string
		typ     string
		payload proto.Message
	}{
		{"no owner", uuid.NewString(), "job.added", &kagamiv1.JobAdded{JobId: "j"}},
		{"owner is not a uuid", uuid.NewString(), "job.added", &kagamiv1.JobAdded{OwnerId: "nope"}},
		{"payload of another type", uuid.NewString(), "job.added", wrapperspb.String("x")},
		{"contact event without an owner", uuid.NewString(), "contact.added", &kagamiv1.ContactAdded{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newConsumer(t)

			err := c.deliver(t, tt.id, tt.typ, tt.payload, timestamppb.New(when))

			if err != nil || len(c.facts(t)) != 0 {
				t.Fatalf("err %v, facts %d; want a quiet acknowledgement", err, len(c.facts(t)))
			}
		})
	}
}

func TestAFactNeverHoldsMessageTextOrNames(t *testing.T) {
	c := newConsumer(t)
	id := uuid.NewString()

	_ = c.deliver(t, id, "job.added", &kagamiv1.JobAdded{OwnerId: uuid.NewString(), Title: "SECRET TITLE", Company: "SECRET CO", Url: "https://secret.example", Source: "referral"}, timestamppb.New(when))

	var raw string
	if err := c.pool.QueryRow(context.Background(), `SELECT dimension::text FROM facts`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	for _, leak := range []string{"SECRET", "secret", "Acme"} {
		for i := 0; i+len(leak) <= len(raw); i++ {
			if raw[i:i+len(leak)] == leak {
				t.Fatalf("dimension %s leaks %q", raw, leak)
			}
		}
	}
}

// Replaying the same events leaves the rollups unchanged: the done-when of the
// rollup task, through the real inbox and the real rollup.
func TestReplayingTheSameEventsTwiceLeavesRollupsUnchanged(t *testing.T) {
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "sensei")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	log := slog.New(slog.DiscardHandler)
	svc := app.NewService(pool, func() time.Time { return when.AddDate(0, 0, 1) })
	sink, err := bus.NewSinkServer(pool, events.Handlers(svc, log, nil), log)
	if err != nil {
		t.Fatalf("sink: %v", err)
	}
	c := &consumer{sink: sink, pool: pool}
	owner := uuid.NewString()
	type delivery struct {
		id, typ string
		payload proto.Message
	}
	batch := []delivery{
		{uuid.NewString(), "job.added", &kagamiv1.JobAdded{OwnerId: owner, JobId: "j1", Source: "linkedin"}},
		{uuid.NewString(), "job.status_changed", &kagamiv1.JobStatusChanged{OwnerId: owner, JobId: "j1", To: kagamiv1.JobStatus_JOB_STATUS_APPLIED}},
		{uuid.NewString(), "job.status_changed", &kagamiv1.JobStatusChanged{OwnerId: owner, JobId: "j1", To: kagamiv1.JobStatus_JOB_STATUS_INTERVIEW}},
		{uuid.NewString(), "contact.status_changed", &kagamiv1.ContactStatusChanged{OwnerId: owner, To: kagamiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT, Channel: "email"}},
	}
	snapshot := func() string {
		rows, err := pool.Query(ctx, `SELECT day::text || metric || dimension || value::text FROM daily_rollups ORDER BY 1`)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := ""
		for rows.Next() {
			var s string
			if err := rows.Scan(&s); err != nil {
				t.Fatal(err)
			}
			out += s + "|"
		}
		return out
	}

	for _, d := range batch {
		if err := c.deliver(t, d.id, d.typ, d.payload, timestamppb.New(when)); err != nil {
			t.Fatalf("deliver %s: %v", d.typ, err)
		}
	}
	if err := svc.Rollup(ctx); err != nil {
		t.Fatalf("rollup: %v", err)
	}
	once := snapshot()
	for _, d := range batch { // the bus delivers at least once: every event again
		if err := c.deliver(t, d.id, d.typ, d.payload, timestamppb.New(when)); err != nil {
			t.Fatalf("redeliver %s: %v", d.typ, err)
		}
	}
	if err := svc.Rollup(ctx); err != nil {
		t.Fatalf("rollup again: %v", err)
	}

	if once == "" || once != snapshot() || len(c.facts(t)) != 4 {
		t.Fatalf("rollups changed after a replay:\nbefore %s\nafter  %s\nfacts %d", once, snapshot(), len(c.facts(t)))
	}
}
