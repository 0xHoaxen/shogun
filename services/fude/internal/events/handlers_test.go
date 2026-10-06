package events_test

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
	"google.golang.org/protobuf/types/known/wrapperspb"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
	"github.com/0xHoaxen/shogun/services/fude/internal/events"
	"github.com/0xHoaxen/shogun/services/fude/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

type fakeQueue struct {
	mu   sync.Mutex
	jobs []app.GenerateArgs
}

func (q *fakeQueue) EnqueueGenerate(_ context.Context, _ pgx.Tx, args app.GenerateArgs) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.jobs = append(q.jobs, args)
	return nil
}

func (q *fakeQueue) EnqueueEmbed(context.Context, pgx.Tx, uuid.UUID) error { return nil }

type consumer struct {
	sink  *bus.SinkServer
	pool  *pgxpool.Pool
	queue *fakeQueue
}

func newConsumer(t *testing.T) *consumer {
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
	queue := &fakeQueue{}
	log := slog.New(slog.DiscardHandler)
	sink, err := bus.NewSinkServer(pool, events.Handlers(app.NewService(pool, queue, nil), log), log)
	if err != nil {
		t.Fatalf("sink: %v", err)
	}
	return &consumer{sink: sink, pool: pool, queue: queue}
}

func (c *consumer) deliver(t *testing.T, id, typ string, payload proto.Message) error {
	t.Helper()
	packed, err := anypb.New(payload)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	_, err = c.sink.Deliver(context.Background(), &eventsv1.DeliverRequest{Envelope: &eventsv1.Envelope{
		Id: id, Type: typ, Source: "kagami", Payload: packed,
	}})
	return err
}

type draftRow struct{ kind, targetType, channel, state string }

func (c *consumer) drafts(t *testing.T) []draftRow {
	t.Helper()
	rows, err := c.pool.Query(context.Background(), `SELECT kind, target_type, channel, state FROM drafts ORDER BY created_at`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rows.Close()
	var out []draftRow
	for rows.Next() {
		var r draftRow
		if err := rows.Scan(&r.kind, &r.targetType, &r.channel, &r.state); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestJobAddedQueuesACopyOnlyCoverLetterForTheOwner(t *testing.T) {
	c := newConsumer(t)
	owner, job := uuid.New(), uuid.New()

	err := c.deliver(t, uuid.NewString(), "job.added", &kagamiv1.JobAdded{OwnerId: owner.String(), JobId: job.String(), Title: "Backend"})

	got := c.drafts(t)
	if err != nil || len(got) != 1 || got[0] != (draftRow{"cover_letter", "job", "other", "generating"}) {
		t.Fatalf("err %v, drafts %+v", err, got)
	}
	if len(c.queue.jobs) != 1 || c.queue.jobs[0].OwnerID != owner || c.queue.jobs[0].Version != 1 {
		t.Fatalf("got jobs %+v", c.queue.jobs)
	}
	var target uuid.UUID
	if err := c.pool.QueryRow(context.Background(), `SELECT target_id FROM drafts`).Scan(&target); err != nil || target != job {
		t.Fatalf("target %s, err %v; want %s", target, err, job)
	}
}

func TestContactStatusChangedQueuesOutreachOnThePreferredChannel(t *testing.T) {
	tests := []struct {
		preferred string
		want      string
	}{
		{"email", "email"}, {"linkedin", "linkedin"}, {"x", "x"}, {"phone", "other"}, {"", "other"},
	}
	for _, tt := range tests {
		t.Run("preferred "+tt.preferred, func(t *testing.T) {
			c := newConsumer(t)

			err := c.deliver(t, uuid.NewString(), "contact.status_changed", &kagamiv1.ContactStatusChanged{
				OwnerId: uuid.NewString(), ContactId: uuid.NewString(), Channel: tt.preferred,
				To: kagamiv1.ContactStatus_CONTACT_STATUS_REPLIED,
			})

			got := c.drafts(t)
			if err != nil || len(got) != 1 || got[0] != (draftRow{"outreach", "contact", tt.want, "generating"}) {
				t.Fatalf("err %v, drafts %+v", err, got)
			}
		})
	}
}

func TestADuplicateDeliveryCreatesOneDraft(t *testing.T) {
	c := newConsumer(t)
	payload := &kagamiv1.JobAdded{OwnerId: uuid.NewString(), JobId: uuid.NewString()}
	id := uuid.NewString()

	first := c.deliver(t, id, "job.added", payload)
	again := c.deliver(t, id, "job.added", payload)

	if first != nil || again != nil || len(c.drafts(t)) != 1 || len(c.queue.jobs) != 1 {
		t.Fatalf("errs %v %v, drafts %d, jobs %d; want one of each", first, again, len(c.drafts(t)), len(c.queue.jobs))
	}
}

func TestADifferentEventForTheSameJobCreatesAnotherDraft(t *testing.T) {
	c := newConsumer(t)
	payload := &kagamiv1.JobAdded{OwnerId: uuid.NewString(), JobId: uuid.NewString()}

	_ = c.deliver(t, uuid.NewString(), "job.added", payload)
	_ = c.deliver(t, uuid.NewString(), "job.added", payload)

	if n := len(c.drafts(t)); n != 2 {
		t.Fatalf("got %d drafts, want one per event", n)
	}
}

func TestEventsThatCannotBeHandledAreAcknowledgedWithoutADraft(t *testing.T) {
	tests := []struct {
		name    string
		typ     string
		payload proto.Message
	}{
		{"job.added without an owner", "job.added", &kagamiv1.JobAdded{JobId: uuid.NewString()}},
		{"job.added with a bad job id", "job.added", &kagamiv1.JobAdded{OwnerId: uuid.NewString(), JobId: "nope"}},
		{"contact event without an owner", "contact.status_changed", &kagamiv1.ContactStatusChanged{ContactId: uuid.NewString()}},
		{"payload of another type", "job.added", wrapperspb.String("x")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newConsumer(t)

			err := c.deliver(t, uuid.NewString(), tt.typ, tt.payload)

			if err != nil || len(c.drafts(t)) != 0 || len(c.queue.jobs) != 0 {
				t.Fatalf("err %v, drafts %d, jobs %d; want a quiet acknowledgement", err, len(c.drafts(t)), len(c.queue.jobs))
			}
		})
	}
}
