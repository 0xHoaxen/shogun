package events_test

import (
	"context"
	"log/slog"
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
	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/kagami/internal/app"
	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
	"github.com/0xHoaxen/shogun/services/kagami/internal/events"
	"github.com/0xHoaxen/shogun/services/kagami/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

// base is the service's clock, and so the time of anything done by hand.
var base = time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)

type consumer struct {
	sink  *bus.SinkServer
	pool  *pgxpool.Pool
	svc   *app.Service
	owner uuid.UUID
	ctx   context.Context
}

func newConsumer(t *testing.T) *consumer {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "kagami")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	svc := app.NewService(pool, func() time.Time { return base })
	log := slog.New(slog.DiscardHandler)
	sink, err := bus.NewSinkServer(pool, events.Handlers(svc, log), log)
	if err != nil {
		t.Fatal(err)
	}
	owner := uuid.New()
	return &consumer{
		sink: sink, pool: pool, svc: svc, owner: owner,
		ctx: authz.WithIdentity(ctx, authz.Identity{OwnerID: owner.String(), RequestID: "test"}),
	}
}

func (c *consumer) deliver(t *testing.T, eventID, typ string, at time.Time, payload proto.Message) error {
	t.Helper()
	packed, err := anypb.New(payload)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	_, err = c.sink.Deliver(context.Background(), &eventsv1.DeliverRequest{Envelope: &eventsv1.Envelope{
		Id: eventID, Type: typ, Source: "tsubame", OccurredAt: timestamppb.New(at), Payload: packed,
	}})
	return err
}

func (c *consumer) job(t *testing.T, status domain.JobStatus) uuid.UUID {
	t.Helper()
	res, err := c.svc.AddJob(c.ctx, app.AddJobInput{Title: "Backend " + uuid.NewString()[:4], CompanyName: "Lumen", Status: status})
	if err != nil {
		t.Fatalf("add job: %v", err)
	}
	return res.Job.ID
}

func (c *consumer) contact(t *testing.T, status domain.ContactStatus, lastContacted string) uuid.UUID {
	t.Helper()
	ct, err := c.svc.AddContact(c.ctx, app.AddContactInput{ContactFields: app.ContactFields{FullName: "Priya " + uuid.NewString()[:4]}, CompanyName: "Lumen"})
	if err != nil {
		t.Fatalf("add contact: %v", err)
	}
	var last any
	if lastContacted != "" {
		last = lastContacted
	}
	if _, err := c.pool.Exec(context.Background(), `UPDATE contacts SET status = $2, last_contacted = $3 WHERE id = $1`, ct.ID, string(status), last); err != nil {
		t.Fatal(err)
	}
	return ct.ID
}

func (c *consumer) status(t *testing.T, table string, id uuid.UUID) string {
	t.Helper()
	var s string
	if err := c.pool.QueryRow(context.Background(), `SELECT status FROM `+table+` WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	return s
}

func (c *consumer) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := c.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (c *consumer) classified(t *testing.T, job uuid.UUID, class tsubamev1.MailClass, confidence float32, at time.Time) error {
	t.Helper()
	return c.deliver(t, uuid.NewString(), "mail.classified", at, &tsubamev1.MailClassified{
		OwnerId: c.owner.String(), MessageId: "msg-" + uuid.NewString()[:6], Classification: class, Confidence: confidence, JobId: job.String(),
	})
}

const (
	confirmation = tsubamev1.MailClass_MAIL_CLASS_APPLICATION_CONFIRMATION
	invite       = tsubamev1.MailClass_MAIL_CLASS_INTERVIEW_INVITE
	rejection    = tsubamev1.MailClass_MAIL_CLASS_REJECTION
	offer        = tsubamev1.MailClass_MAIL_CLASS_OFFER
	other        = tsubamev1.MailClass_MAIL_CLASS_OTHER
)

func TestASureMailMovesTheJobAndRecordsWhyOnItsTimeline(t *testing.T) {
	c := newConsumer(t)
	job := c.job(t, domain.JobSaved)

	err := c.classified(t, job, confirmation, 0.95, base.Add(time.Minute))

	if got := c.status(t, "jobs", job); err != nil || got != "applied" {
		t.Fatalf("err %v, status %q", err, got)
	}
	if n := c.count(t, `SELECT count(*) FROM job_events WHERE job_id = $1 AND kind = 'status_changed' AND payload->>'source' = 'mail'`, job); n != 1 {
		t.Fatalf("got %d mail status entries, want 1", n)
	}
	if n := c.count(t, `SELECT count(*) FROM job_events WHERE job_id = $1 AND kind = 'mail_linked'`, job); n != 1 {
		t.Fatalf("got %d mail_linked entries, want 1", n)
	}
	if n := c.count(t, `SELECT count(*) FROM outbox WHERE type = 'job.status_changed'`); n != 1 {
		t.Fatalf("got %d job.status_changed events, want 1", n)
	}
}

func TestAnUnsureMailOnlyLeavesANoteAndMovesNothing(t *testing.T) {
	c := newConsumer(t)
	job := c.job(t, domain.JobApplied)

	err := c.classified(t, job, rejection, 0.8, base.Add(time.Minute))

	if got := c.status(t, "jobs", job); err != nil || got != "applied" {
		t.Fatalf("err %v, status %q; below 0.9 mail is only a suggestion", err, got)
	}
	if n := c.count(t, `SELECT count(*) FROM job_events WHERE job_id = $1 AND kind = 'mail_linked'`, job); n != 1 {
		t.Fatalf("got %d mail_linked entries", n)
	}
	if n := c.count(t, `SELECT count(*) FROM outbox WHERE type = 'job.status_changed'`); n != 0 {
		t.Fatalf("got %d job.status_changed events", n)
	}
}

func TestMailMovesAJobOnlyWhereTheStateMachineAllows(t *testing.T) {
	tests := []struct {
		name   string
		from   domain.JobStatus
		class  tsubamev1.MailClass
		want   string
		reason string
	}{
		{"applied to interview", domain.JobApplied, invite, "interview", ""},
		{"interview to offer", domain.JobInterview, offer, "offer", ""},
		{"applied to rejected", domain.JobApplied, rejection, "rejected", ""},
		{"saved cannot jump to an offer", domain.JobSaved, offer, "saved", "not allowed"},
		{"saved cannot jump to an interview", domain.JobSaved, invite, "saved", "not allowed"},
		{"a rejected job is never reopened by mail", domain.JobRejected, confirmation, "rejected", "owner only"},
		{"a second invite is not a new status", domain.JobInterview, invite, "interview", "same status"},
		{"a reply moves no job", domain.JobApplied, other, "applied", "not about progress"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newConsumer(t)
			job := c.job(t, tt.from)

			err := c.classified(t, job, tt.class, 0.95, base.Add(time.Minute))

			if got := c.status(t, "jobs", job); err != nil || got != tt.want {
				t.Fatalf("err %v, status %q, want %q (%s)", err, got, tt.want, tt.reason)
			}
		})
	}
}

func TestEventsArrivingOutOfOrderCannotUndoANewerOne(t *testing.T) {
	c := newConsumer(t)
	job := c.job(t, domain.JobApplied)
	invited := base.Add(5 * time.Minute)

	first := c.classified(t, job, invite, 0.95, invited)
	older := c.classified(t, job, rejection, 0.95, base.Add(time.Minute)) // occurred before the invite, arrives after
	afterwards := c.status(t, "jobs", job)
	newer := c.classified(t, job, rejection, 0.95, base.Add(10*time.Minute))

	if first != nil || older != nil || newer != nil {
		t.Fatalf("errors %v %v %v", first, older, newer)
	}
	if afterwards != "interview" {
		t.Fatalf("the stale rejection undid the newer invite: status %q", afterwards)
	}
	if got := c.status(t, "jobs", job); got != "rejected" {
		t.Fatalf("a genuinely newer rejection must apply: status %q", got)
	}
}

func TestMailOlderThanTheOwnersLastChangeIsIgnored(t *testing.T) {
	c := newConsumer(t)
	job := c.job(t, domain.JobSaved)
	if _, err := c.svc.ChangeJobStatus(c.ctx, app.ChangeJobStatusInput{ID: job.String(), To: domain.JobApplied, Version: 1}); err != nil {
		t.Fatalf("manual change: %v", err)
	}

	old := c.classified(t, job, invite, 0.95, base.Add(-time.Hour)) // before the owner acted
	stale := c.status(t, "jobs", job)
	fresh := c.classified(t, job, invite, 0.95, base.Add(time.Hour))

	if old != nil || fresh != nil || stale != "applied" || c.status(t, "jobs", job) != "interview" {
		t.Fatalf("errors %v %v, status after old mail %q, after new %q", old, fresh, stale, c.status(t, "jobs", job))
	}
}

func TestADuplicateDeliveryMovesTheJobOnce(t *testing.T) {
	c := newConsumer(t)
	job := c.job(t, domain.JobSaved)
	event := uuid.NewString()
	payload := &tsubamev1.MailClassified{OwnerId: c.owner.String(), MessageId: "m1", Classification: confirmation, Confidence: 0.95, JobId: job.String()}

	first := c.deliver(t, event, "mail.classified", base.Add(time.Minute), payload)
	again := c.deliver(t, event, "mail.classified", base.Add(time.Minute), payload)

	if first != nil || again != nil || c.count(t, `SELECT count(*) FROM job_events WHERE job_id = $1 AND kind = 'status_changed'`, job) != 1 {
		t.Fatalf("errs %v %v", first, again)
	}
	if n := c.count(t, `SELECT count(*) FROM outbox WHERE type = 'job.status_changed'`); n != 1 {
		t.Fatalf("got %d status events, want 1", n)
	}
}

func TestMailWithoutAJobOrForAJobThatIsGoneChangesNothing(t *testing.T) {
	c := newConsumer(t)
	noJob := c.deliver(t, uuid.NewString(), "mail.classified", base, &tsubamev1.MailClassified{OwnerId: c.owner.String(), Classification: offer, Confidence: 1})
	gone := c.classified(t, uuid.New(), offer, 1, base)
	another := newConsumer(t)
	foreign := another.job(t, domain.JobApplied)
	notMine := c.classified(t, foreign, rejection, 1, base) // a job in another database stands for another owner's job

	if noJob != nil || gone != nil || notMine != nil {
		t.Fatalf("errors %v %v %v; these must be quiet acknowledgements", noJob, gone, notMine)
	}
	if c.count(t, `SELECT count(*) FROM outbox WHERE type = 'job.status_changed'`) != 0 {
		t.Fatal("something moved")
	}
}

func TestEventsThatCannotBeReadAreAcknowledgedAndDropped(t *testing.T) {
	c := newConsumer(t)
	tests := []struct {
		name    string
		typ     string
		payload proto.Message
	}{
		{"mail.classified without an owner", "mail.classified", &tsubamev1.MailClassified{JobId: uuid.NewString()}},
		{"mail.classified with a bad job id", "mail.classified", &tsubamev1.MailClassified{OwnerId: c.owner.String(), JobId: "nope"}},
		{"mail.reply_detected without a contact", "mail.reply_detected", &tsubamev1.MailReplyDetected{OwnerId: c.owner.String()}},
		{"draft.sent without an owner", "draft.sent", &tsubamev1.DraftSent{ContactId: uuid.NewString()}},
		{"draft.sent with a bad contact id", "draft.sent", &tsubamev1.DraftSent{OwnerId: c.owner.String(), ContactId: "nope"}},
		{"a payload of another type", "mail.classified", wrapperspb.String("x")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := c.deliver(t, uuid.NewString(), tt.typ, base, tt.payload); err != nil {
				t.Fatalf("got %v, want a quiet acknowledgement", err)
			}
		})
	}
}

func (c *consumer) replied(t *testing.T, contact uuid.UUID, at time.Time) error {
	t.Helper()
	return c.deliver(t, uuid.NewString(), "mail.reply_detected", at, &tsubamev1.MailReplyDetected{
		OwnerId: c.owner.String(), MessageId: "msg-reply", ContactId: contact.String(),
	})
}

func TestAReplyMovesAReachedContactToRepliedAndSaysSo(t *testing.T) {
	for _, from := range []domain.ContactStatus{domain.ContactReachedOut, domain.ContactConversationStarted, domain.ContactReferralAsked} {
		t.Run(string(from), func(t *testing.T) {
			c := newConsumer(t)
			contact := c.contact(t, from, "")

			err := c.replied(t, contact, base.Add(time.Minute))

			if got := c.status(t, "contacts", contact); err != nil || got != "replied" {
				t.Fatalf("err %v, status %q", err, got)
			}
			if n := c.count(t, `SELECT count(*) FROM contact_events WHERE contact_id = $1 AND kind = 'reply_received'`, contact); n != 1 {
				t.Fatalf("got %d reply_received entries", n)
			}
			if n := c.count(t, `SELECT count(*) FROM outbox WHERE type = 'contact.status_changed'`); n != 1 {
				t.Fatalf("got %d contact.status_changed events", n)
			}
		})
	}
}

func TestAReplyFromAContactNotYetReachedIsRecordedButMovesNothing(t *testing.T) {
	c := newConsumer(t)
	contact := c.contact(t, domain.ContactNotReached, "")

	err := c.replied(t, contact, base.Add(time.Minute))

	if got := c.status(t, "contacts", contact); err != nil || got != "not_reached" {
		t.Fatalf("err %v, status %q", err, got)
	}
	if n := c.count(t, `SELECT count(*) FROM contact_events WHERE contact_id = $1 AND kind = 'reply_received'`, contact); n != 1 {
		t.Fatalf("got %d reply_received entries; the reply should still be on the timeline", n)
	}
}

func TestAReplyOlderThanAnAlreadyRecordedChangeIsIgnored(t *testing.T) {
	c := newConsumer(t)
	contact := c.contact(t, domain.ContactReachedOut, "")
	if _, err := c.svc.ChangeContactStatus(c.ctx, app.ChangeContactStatusInput{ID: contact.String(), To: domain.ContactConversationStarted, Version: 1}); err != nil {
		t.Fatalf("manual change: %v", err)
	}

	err := c.replied(t, contact, base.Add(-time.Hour))

	if got := c.status(t, "contacts", contact); err != nil || got != "conversation_started" {
		t.Fatalf("err %v, status %q; an older reply must not undo what the owner did", err, got)
	}
}

func (c *consumer) sent(t *testing.T, contact uuid.UUID, at time.Time) error {
	t.Helper()
	return c.deliver(t, uuid.NewString(), "draft.sent", at, &tsubamev1.DraftSent{
		OwnerId: c.owner.String(), DraftId: uuid.NewString(), Version: 1, ContactId: contact.String(), SentAt: timestamppb.New(at),
	})
}

func (c *consumer) lastContacted(t *testing.T, id uuid.UUID) string {
	t.Helper()
	var s *string
	if err := c.pool.QueryRow(context.Background(), `SELECT to_char(last_contacted, 'YYYY-MM-DD') FROM contacts WHERE id = $1`, id).Scan(&s); err != nil {
		t.Fatal(err)
	}
	if s == nil {
		return ""
	}
	return *s
}

func TestASentDraftReachesAContactAndSetsTheDayLastContacted(t *testing.T) {
	c := newConsumer(t)
	contact := c.contact(t, domain.ContactNotReached, "")

	err := c.sent(t, contact, time.Date(2026, 10, 6, 15, 30, 0, 0, time.UTC))

	if got := c.status(t, "contacts", contact); err != nil || got != "reached_out" || c.lastContacted(t, contact) != "2026-10-06" {
		t.Fatalf("err %v, status %q, last contacted %q", err, got, c.lastContacted(t, contact))
	}
	if n := c.count(t, `SELECT count(*) FROM contact_events WHERE contact_id = $1 AND kind = 'message_sent'`, contact); n != 1 {
		t.Fatalf("got %d message_sent entries", n)
	}
}

func TestASentDraftNeverMovesLastContactedBackOrChangesALaterStatus(t *testing.T) {
	c := newConsumer(t)
	contact := c.contact(t, domain.ContactReplied, "2026-10-10")

	older := c.sent(t, contact, time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC))
	afterOlder := c.lastContacted(t, contact)
	newer := c.sent(t, contact, time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC))

	if older != nil || newer != nil || afterOlder != "2026-10-10" || c.lastContacted(t, contact) != "2026-10-12" {
		t.Fatalf("errs %v %v, after older %q, after newer %q", older, newer, afterOlder, c.lastContacted(t, contact))
	}
	if got := c.status(t, "contacts", contact); got != "replied" {
		t.Fatalf("status %q; a sent draft only moves a contact who was not reached", got)
	}
}

func TestASentDraftForNoContactOrAMissingContactChangesNothing(t *testing.T) {
	c := newConsumer(t)

	none := c.deliver(t, uuid.NewString(), "draft.sent", base, &tsubamev1.DraftSent{OwnerId: c.owner.String(), DraftId: uuid.NewString(), Version: 1})
	missing := c.sent(t, uuid.New(), base)

	if none != nil || missing != nil {
		t.Fatalf("errors %v %v", none, missing)
	}
}

func TestADuplicateDraftSentDeliveryRecordsOnce(t *testing.T) {
	c := newConsumer(t)
	contact := c.contact(t, domain.ContactNotReached, "")
	event := uuid.NewString()
	payload := &tsubamev1.DraftSent{OwnerId: c.owner.String(), DraftId: "d1", Version: 1, ContactId: contact.String(), SentAt: timestamppb.New(base)}

	_ = c.deliver(t, event, "draft.sent", base, payload)
	_ = c.deliver(t, event, "draft.sent", base, payload)

	if n := c.count(t, `SELECT count(*) FROM contact_events WHERE contact_id = $1 AND kind = 'message_sent'`, contact); n != 1 {
		t.Fatalf("got %d message_sent entries, want 1", n)
	}
}
