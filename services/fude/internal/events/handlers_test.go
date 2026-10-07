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

	dojov1 "github.com/0xHoaxen/shogun/gen/go/shogun/dojo/v1"
	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
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
		{"activity without an owner", "learning.activity_added", &dojov1.LearningActivityAdded{ActivityId: uuid.NewString()}},
		{"activity with a bad id", "learning.activity_added", &dojov1.LearningActivityAdded{OwnerId: uuid.NewString(), ActivityId: "nope"}},
		{"item without an owner", "learning.item_completed", &dojov1.LearningItemCompleted{ItemId: uuid.NewString()}},
		{"item with a bad id", "learning.item_completed", &dojov1.LearningItemCompleted{OwnerId: uuid.NewString(), ItemId: "nope"}},
		{"learning payload of another type", "learning.activity_added", wrapperspb.String("x")},
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

// draftIn stores a draft in a state on a version and returns its id.
func (c *consumer) draftIn(t *testing.T, owner uuid.UUID, state string, version int32) uuid.UUID {
	t.Helper()
	id := uuid.New()
	if _, err := c.pool.Exec(context.Background(), `INSERT INTO drafts (id, owner_id, kind, target_type, channel, state, current_version)
		VALUES ($1, $2, 'cover_letter', 'job', 'email', $3, $4)`, id, owner, state, version); err != nil {
		t.Fatal(err)
	}
	return id
}

func (c *consumer) stateOf(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var state string
	if err := c.pool.QueryRow(context.Background(), `SELECT state FROM drafts WHERE id = $1`, id).Scan(&state); err != nil {
		t.Fatal(err)
	}
	return state
}

func sentEvent(owner, draft uuid.UUID, version int32) *tsubamev1.DraftSent {
	return &tsubamev1.DraftSent{OwnerId: owner.String(), DraftId: draft.String(), Version: version}
}

func failedEvent(owner, draft uuid.UUID, version int32) *tsubamev1.DraftSendFailed {
	return &tsubamev1.DraftSendFailed{OwnerId: owner.String(), DraftId: draft.String(), Version: version, Reason: "provider_error"}
}

func TestDraftSentMovesAnApprovedDraftToSent(t *testing.T) {
	c := newConsumer(t)
	owner := uuid.New()
	id := c.draftIn(t, owner, "approved", 2)

	err := c.deliver(t, uuid.NewString(), "draft.sent", sentEvent(owner, id, 2))

	if got := c.stateOf(t, id); err != nil || got != "sent" {
		t.Fatalf("err %v, state %q", err, got)
	}
}

func TestDraftSendFailedPutsAnApprovedDraftBackToPending(t *testing.T) {
	c := newConsumer(t)
	owner := uuid.New()
	id := c.draftIn(t, owner, "approved", 2)

	err := c.deliver(t, uuid.NewString(), "draft.send_failed", failedEvent(owner, id, 2))

	if got := c.stateOf(t, id); err != nil || got != "pending" {
		t.Fatalf("err %v, state %q", err, got)
	}
}

func TestADuplicateOutcomeDeliveryChangesNothingTheSecondTime(t *testing.T) {
	c := newConsumer(t)
	owner := uuid.New()
	id := c.draftIn(t, owner, "approved", 2)
	event := uuid.NewString()

	first := c.deliver(t, event, "draft.sent", sentEvent(owner, id, 2))
	again := c.deliver(t, event, "draft.sent", sentEvent(owner, id, 2))

	if got := c.stateOf(t, id); first != nil || again != nil || got != "sent" {
		t.Fatalf("errs %v %v, state %q", first, again, got)
	}
}

func TestALateDraftSentForADraftPutBackToPendingStillRecordsThatTheMailWentOut(t *testing.T) {
	c := newConsumer(t)
	owner := uuid.New()
	id := c.draftIn(t, owner, "pending", 2) // reverted after a send that looked lost

	err := c.deliver(t, uuid.NewString(), "draft.sent", sentEvent(owner, id, 2))

	if got := c.stateOf(t, id); err != nil || got != "sent" {
		t.Fatalf("err %v, state %q; the mail is out, so the draft must say so", err, got)
	}
}

func TestStaleOrIrrelevantOutcomesAreAcknowledgedAndChangeNothing(t *testing.T) {
	owner := uuid.New()
	tests := []struct {
		name      string
		state     string
		version   int32
		eventType string
		payload   func(owner, id uuid.UUID) proto.Message
		want      string
	}{
		{"sent for an older version", "approved", 3, "draft.sent", func(o, id uuid.UUID) proto.Message { return sentEvent(o, id, 2) }, "approved"},
		{"send_failed for an older version", "approved", 3, "draft.send_failed", func(o, id uuid.UUID) proto.Message { return failedEvent(o, id, 2) }, "approved"},
		{"send_failed after the draft was edited back to pending", "pending", 3, "draft.send_failed", func(o, id uuid.UUID) proto.Message { return failedEvent(o, id, 2) }, "pending"},
		{"send_failed for a draft already sent", "sent", 2, "draft.send_failed", func(o, id uuid.UUID) proto.Message { return failedEvent(o, id, 2) }, "sent"},
		{"sent for a discarded draft", "discarded", 2, "draft.sent", func(o, id uuid.UUID) proto.Message { return sentEvent(o, id, 2) }, "discarded"},
		{"sent for a draft still generating", "generating", 2, "draft.sent", func(o, id uuid.UUID) proto.Message { return sentEvent(o, id, 2) }, "generating"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newConsumer(t)
			id := c.draftIn(t, owner, tt.state, tt.version)

			err := c.deliver(t, uuid.NewString(), tt.eventType, tt.payload(owner, id))

			if got := c.stateOf(t, id); err != nil || got != tt.want {
				t.Fatalf("err %v, state %q, want %q", err, got, tt.want)
			}
		})
	}
}

func TestOutcomesForAnotherOwnerOrAMissingDraftOrBadIDsAreAcknowledgedQuietly(t *testing.T) {
	c := newConsumer(t)
	owner := uuid.New()
	id := c.draftIn(t, owner, "approved", 2)
	cases := []struct {
		name    string
		typ     string
		payload proto.Message
	}{
		{"another owner's draft", "draft.sent", sentEvent(uuid.New(), id, 2)},
		{"a draft that is gone", "draft.sent", sentEvent(owner, uuid.New(), 2)},
		{"a failure for a draft that is gone", "draft.send_failed", failedEvent(owner, uuid.New(), 2)},
		{"a bad draft id", "draft.sent", &tsubamev1.DraftSent{OwnerId: owner.String(), DraftId: "nope", Version: 2}},
		{"no owner", "draft.send_failed", &tsubamev1.DraftSendFailed{DraftId: id.String(), Version: 2}},
		{"a payload of another type", "draft.sent", wrapperspb.String("x")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := c.deliver(t, uuid.NewString(), tc.typ, tc.payload); err != nil {
				t.Fatalf("got %v, want a quiet acknowledgement", err)
			}
		})
	}
	if got := c.stateOf(t, id); got != "approved" {
		t.Fatalf("state %q: none of those events concerned this draft", got)
	}
}

func TestLearningEventsQueueALinkedInPostAboutTheirTarget(t *testing.T) {
	owner, target := uuid.New(), uuid.New()
	tests := []struct {
		name    string
		typ     string
		payload proto.Message
	}{
		{"activity added", "learning.activity_added", &dojov1.LearningActivityAdded{OwnerId: owner.String(), ActivityId: target.String(), Summary: "built a pool"}},
		{"item completed", "learning.item_completed", &dojov1.LearningItemCompleted{OwnerId: owner.String(), ItemId: target.String(), Title: "Go course"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newConsumer(t)

			err := c.deliver(t, uuid.NewString(), tt.typ, tt.payload)

			got := c.drafts(t)
			if err != nil || len(got) != 1 || got[0] != (draftRow{"post", "learning_activity", "linkedin", "generating"}) {
				t.Fatalf("err %v, drafts %+v", err, got)
			}
			if len(c.queue.jobs) != 1 || c.queue.jobs[0].OwnerID != owner || c.queue.jobs[0].Version != 1 {
				t.Fatalf("got jobs %+v", c.queue.jobs)
			}
			var stored uuid.UUID
			if err := c.pool.QueryRow(context.Background(), `SELECT target_id FROM drafts`).Scan(&stored); err != nil || stored != target {
				t.Fatalf("target %s, err %v; want %s", stored, err, target)
			}
		})
	}
}

func TestADuplicateLearningDeliveryCreatesOnePost(t *testing.T) {
	c := newConsumer(t)
	payload := &dojov1.LearningActivityAdded{OwnerId: uuid.NewString(), ActivityId: uuid.NewString()}
	id := uuid.NewString()

	first := c.deliver(t, id, "learning.activity_added", payload)
	again := c.deliver(t, id, "learning.activity_added", payload)

	if first != nil || again != nil || len(c.drafts(t)) != 1 || len(c.queue.jobs) != 1 {
		t.Fatalf("errs %v %v, drafts %d, jobs %d; want one of each", first, again, len(c.drafts(t)), len(c.queue.jobs))
	}
}
