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
	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/taiko/internal/app"
	"github.com/0xHoaxen/shogun/services/taiko/internal/events"
	"github.com/0xHoaxen/shogun/services/taiko/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

type consumer struct {
	sink *bus.SinkServer
	pool *pgxpool.Pool
}

func newConsumer(t *testing.T) *consumer {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "taiko")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	log := slog.New(slog.DiscardHandler)
	sink, err := bus.NewSinkServer(pool, events.Handlers(app.NewService(pool, nil), log), log)
	if err != nil {
		t.Fatalf("sink: %v", err)
	}
	return &consumer{sink: sink, pool: pool}
}

func (c *consumer) deliver(t *testing.T, id, typ string, payload proto.Message) error {
	t.Helper()
	packed, err := anypb.New(payload)
	if err != nil {
		t.Fatalf("pack: %v", err)
	}
	_, err = c.sink.Deliver(context.Background(), &eventsv1.DeliverRequest{Envelope: &eventsv1.Envelope{
		Id: id, Type: typ, Source: "test", Payload: packed,
	}})
	return err
}

type row struct{ owner, typ, title, link, sourceEvent string }

func (c *consumer) rows(t *testing.T) []row {
	t.Helper()
	rs, err := c.pool.Query(context.Background(),
		`SELECT owner_id::text, type, title, COALESCE(link, ''), source_event_id::text FROM notifications ORDER BY created_at, id`)
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	defer rs.Close()
	var out []row
	for rs.Next() {
		var r row
		if err := rs.Scan(&r.owner, &r.typ, &r.title, &r.link, &r.sourceEvent); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestEachEventTypeCreatesItsNotificationForTheOwner(t *testing.T) {
	owner, draft := uuid.NewString(), uuid.NewString()
	tests := []struct {
		name      string
		typ       string
		payload   proto.Message
		wantType  string
		wantTitle string
		wantLink  string
	}{
		{
			"job follow-up", "job.follow_up_due", &kagamiv1.JobFollowUpDue{OwnerId: owner, JobId: uuid.NewString(), DueOn: "2026-10-03"},
			"follow_up_due", "Job follow-up due", "/jobs",
		},
		{
			"contact follow-up", "contact.follow_up_due", &kagamiv1.ContactFollowUpDue{OwnerId: owner, ContactId: uuid.NewString(), DueOn: "2026-10-03"},
			"follow_up_due", "Contact follow-up due", "/contacts",
		},
		{
			"interview invite mail", "mail.classified", &tsubamev1.MailClassified{OwnerId: owner, Classification: tsubamev1.MailClass_MAIL_CLASS_INTERVIEW_INVITE},
			"interview_invite", "Interview invite received", "/jobs",
		},
		{
			"offer mail", "mail.classified", &tsubamev1.MailClassified{OwnerId: owner, Classification: tsubamev1.MailClass_MAIL_CLASS_OFFER},
			"offer", "Offer received", "/jobs",
		},
		{
			"rejection mail", "mail.classified", &tsubamev1.MailClassified{OwnerId: owner, Classification: tsubamev1.MailClass_MAIL_CLASS_REJECTION},
			"rejection", "Application rejected", "/jobs",
		},
		{
			"reply", "mail.reply_detected", &tsubamev1.MailReplyDetected{OwnerId: owner, ContactId: uuid.NewString()},
			"reply_detected", "A contact replied", "/contacts",
		},
		{
			"draft ready", "draft.ready", &fudev1.DraftReady{OwnerId: owner, DraftId: draft, Kind: fudev1.DraftKind_DRAFT_KIND_COVER_LETTER},
			"draft_ready", "Cover letter ready", "/drafts/" + draft,
		},
		{
			"draft failed", "draft.failed", &fudev1.DraftFailed{OwnerId: owner, DraftId: draft, Reason: "generation_failed"},
			"draft_failed", "Draft generation failed", "/drafts/" + draft,
		},
		{
			"send failed", "draft.send_failed", &tsubamev1.DraftSendFailed{OwnerId: owner, DraftId: draft, Reason: "auth_revoked"},
			"draft_send_failed", "Email was not sent", "/drafts/" + draft,
		},
		{
			"threshold", "cost.threshold_reached", &sorobanv1.CostThresholdReached{
				OwnerId: owner, ScopeType: sorobanv1.ScopeType_SCOPE_TYPE_GLOBAL, Period: sorobanv1.BudgetPeriod_BUDGET_PERIOD_DAILY, Percent: 80,
			},
			"budget_threshold", "Claude spend at 80% of the daily budget", "/settings/spend",
		},
		{
			"exhausted", "cost.budget_exhausted", &sorobanv1.CostBudgetExhausted{
				OwnerId: owner, ScopeType: sorobanv1.ScopeType_SCOPE_TYPE_SERVICE, ScopeValue: "fude",
				Period: sorobanv1.BudgetPeriod_BUDGET_PERIOD_MONTHLY, ResetsAt: timestamppb.New(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)),
			},
			"budget_exhausted", "Claude budget used up", "/settings/spend",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newConsumer(t)
			id := uuid.NewString()

			err := c.deliver(t, id, tt.typ, tt.payload)

			got := c.rows(t)
			want := row{owner: owner, typ: tt.wantType, title: tt.wantTitle, link: tt.wantLink, sourceEvent: id}
			if err != nil || len(got) != 1 || got[0] != want {
				t.Fatalf("err %v, rows %+v; want one row %+v", err, got, want)
			}
		})
	}
}

func TestMailThatNeedsNoAttentionIsAcknowledgedWithoutANotification(t *testing.T) {
	for _, class := range []tsubamev1.MailClass{
		tsubamev1.MailClass_MAIL_CLASS_APPLICATION_CONFIRMATION, tsubamev1.MailClass_MAIL_CLASS_RECRUITER_OUTREACH,
		tsubamev1.MailClass_MAIL_CLASS_REPLY, tsubamev1.MailClass_MAIL_CLASS_OTHER, tsubamev1.MailClass_MAIL_CLASS_UNSPECIFIED,
	} {
		t.Run(class.String(), func(t *testing.T) {
			c := newConsumer(t)

			err := c.deliver(t, uuid.NewString(), "mail.classified",
				&tsubamev1.MailClassified{OwnerId: uuid.NewString(), Classification: class})

			if err != nil || len(c.rows(t)) != 0 {
				t.Fatalf("err %v, rows %d; want an acknowledged event and no rows", err, len(c.rows(t)))
			}
		})
	}
}

func TestADuplicateDeliveryCreatesOneNotification(t *testing.T) {
	c := newConsumer(t)
	payload := &fudev1.DraftReady{OwnerId: uuid.NewString(), DraftId: uuid.NewString(), Kind: fudev1.DraftKind_DRAFT_KIND_OUTREACH}
	id := uuid.NewString()

	first := c.deliver(t, id, "draft.ready", payload)
	again := c.deliver(t, id, "draft.ready", payload)

	if first != nil || again != nil || len(c.rows(t)) != 1 {
		t.Fatalf("errs %v %v, rows %d; want one row", first, again, len(c.rows(t)))
	}
}

func TestADifferentEventAboutTheSameDraftCreatesAnotherNotification(t *testing.T) {
	c := newConsumer(t)
	payload := &fudev1.DraftReady{OwnerId: uuid.NewString(), DraftId: uuid.NewString(), Kind: fudev1.DraftKind_DRAFT_KIND_OUTREACH}

	_ = c.deliver(t, uuid.NewString(), "draft.ready", payload)
	_ = c.deliver(t, uuid.NewString(), "draft.ready", payload)

	if n := len(c.rows(t)); n != 2 {
		t.Fatalf("got %d rows, want one per event", n)
	}
}

func TestEventsThatCannotBeHandledAreAcknowledgedWithoutANotification(t *testing.T) {
	tests := []struct {
		name    string
		typ     string
		payload proto.Message
	}{
		{"no owner", "draft.ready", &fudev1.DraftReady{DraftId: uuid.NewString()}},
		{"owner is not a uuid", "draft.ready", &fudev1.DraftReady{OwnerId: "nope", DraftId: uuid.NewString()}},
		{"draft id is not a uuid", "draft.ready", &fudev1.DraftReady{OwnerId: uuid.NewString(), DraftId: "../etc"}},
		{"draft id could break the link", "draft.failed", &fudev1.DraftFailed{OwnerId: uuid.NewString(), DraftId: "x/../y"}},
		{"send failed without a draft id", "draft.send_failed", &tsubamev1.DraftSendFailed{OwnerId: uuid.NewString()}},
		{"payload of another type", "job.follow_up_due", wrapperspb.String("not a follow-up")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newConsumer(t)

			err := c.deliver(t, uuid.NewString(), tt.typ, tt.payload)

			if err != nil || len(c.rows(t)) != 0 {
				t.Fatalf("err %v, rows %d; want an acknowledged event and no rows", err, len(c.rows(t)))
			}
		})
	}
}
