package events

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	katanav1 "github.com/0xHoaxen/shogun/gen/go/shogun/katana/v1"
	shinobiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/shinobi/v1"
	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/services/taiko/internal/app"
	"github.com/0xHoaxen/shogun/services/taiko/internal/domain"
)

// Event types taiko consumes.
const (
	TypeJobFollowUpDue       = "job.follow_up_due"
	TypeContactFollowUpDue   = "contact.follow_up_due"
	TypeMailClassified       = "mail.classified"
	TypeMailReplyDetected    = "mail.reply_detected"
	TypeDraftReady           = "draft.ready"
	TypeDraftFailed          = "draft.failed"
	TypeDraftSendFailed      = "draft.send_failed"
	TypeCostThresholdReached = "cost.threshold_reached"
	TypeCostBudgetExhausted  = "cost.budget_exhausted"
	TypeProfileSuggestion    = "profile.suggestion_ready"
	TypeDiscoveryMatch       = "discovery.match_found"
)

// Recorder stores the notification an event caused.
type Recorder interface {
	Record(ctx context.Context, tx pgx.Tx, in app.RecordInput) error
}

// Handlers returns taiko's handlers by event type, for bus.NewSinkServer.
func Handlers(rec Recorder, log *slog.Logger) map[string]bus.Handler {
	h := &handlers{rec: rec, log: log}
	return map[string]bus.Handler{
		TypeJobFollowUpDue:       h.jobFollowUpDue,
		TypeContactFollowUpDue:   h.contactFollowUpDue,
		TypeMailClassified:       h.mailClassified,
		TypeMailReplyDetected:    h.mailReplyDetected,
		TypeDraftReady:           h.draftReady,
		TypeDraftFailed:          h.draftFailed,
		TypeDraftSendFailed:      h.draftSendFailed,
		TypeCostThresholdReached: h.costThresholdReached,
		TypeCostBudgetExhausted:  h.costBudgetExhausted,
		TypeProfileSuggestion:    h.profileSuggestion,
		TypeDiscoveryMatch:       h.discoveryMatch,
	}
}

type handlers struct {
	rec Recorder
	log *slog.Logger
}

func (h *handlers) jobFollowUpDue(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p kagamiv1.JobFollowUpDue
	if !h.payload(env, &p) {
		return nil
	}
	return h.record(ctx, tx, env, p.GetOwnerId(), func() (domain.Notice, error) {
		return domain.FollowUpDue(domain.TargetJob, p.GetDueOn())
	})
}

func (h *handlers) contactFollowUpDue(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p kagamiv1.ContactFollowUpDue
	if !h.payload(env, &p) {
		return nil
	}
	return h.record(ctx, tx, env, p.GetOwnerId(), func() (domain.Notice, error) {
		return domain.FollowUpDue(domain.TargetContact, p.GetDueOn())
	})
}

// mailClassified notifies for the classes that need the owner's attention.
// Confirmations, recruiter outreach, replies and other mail are acknowledged
// without a notification.
func (h *handlers) mailClassified(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p tsubamev1.MailClassified
	if !h.payload(env, &p) {
		return nil
	}
	t, ok := mailTypes[p.GetClassification()]
	if !ok {
		return nil
	}
	return h.record(ctx, tx, env, p.GetOwnerId(), func() (domain.Notice, error) { return domain.Mail(t) })
}

var mailTypes = map[tsubamev1.MailClass]domain.Type{
	tsubamev1.MailClass_MAIL_CLASS_INTERVIEW_INVITE: domain.TypeInterviewInvite,
	tsubamev1.MailClass_MAIL_CLASS_OFFER:            domain.TypeOffer,
	tsubamev1.MailClass_MAIL_CLASS_REJECTION:        domain.TypeRejection,
}

func (h *handlers) mailReplyDetected(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p tsubamev1.MailReplyDetected
	if !h.payload(env, &p) {
		return nil
	}
	return h.record(ctx, tx, env, p.GetOwnerId(), domain.ReplyDetected)
}

func (h *handlers) draftReady(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p fudev1.DraftReady
	if !h.payload(env, &p) || !h.uuid(env, "draft id", p.GetDraftId()) {
		return nil
	}
	return h.record(ctx, tx, env, p.GetOwnerId(), func() (domain.Notice, error) {
		return domain.DraftReady(p.GetDraftId(), enumName(p.GetKind(), "DRAFT_KIND_"))
	})
}

func (h *handlers) draftFailed(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p fudev1.DraftFailed
	if !h.payload(env, &p) || !h.uuid(env, "draft id", p.GetDraftId()) {
		return nil
	}
	return h.record(ctx, tx, env, p.GetOwnerId(), func() (domain.Notice, error) {
		return domain.DraftFailed(p.GetDraftId(), p.GetReason())
	})
}

func (h *handlers) discoveryMatch(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p shinobiv1.DiscoveryMatchFound
	if !h.payload(env, &p) {
		return nil
	}
	return h.record(ctx, tx, env, p.GetOwnerId(), func() (domain.Notice, error) {
		return domain.DiscoveryMatch(p.GetTitle(), p.GetCompany(), p.GetScore())
	})
}

func (h *handlers) profileSuggestion(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p katanav1.ProfileSuggestionReady
	if !h.payload(env, &p) {
		return nil
	}
	return h.record(ctx, tx, env, p.GetOwnerId(), func() (domain.Notice, error) {
		return domain.ProfileSuggestion(suggestionTarget(p.GetTarget()))
	})
}

// suggestionTarget names katana's target the way the builder expects, or ""
// for one it does not know.
func suggestionTarget(t katanav1.SuggestionTarget) string {
	switch t {
	case katanav1.SuggestionTarget_SUGGESTION_TARGET_RESUME:
		return domain.SuggestionResume
	case katanav1.SuggestionTarget_SUGGESTION_TARGET_LINKEDIN:
		return domain.SuggestionLinkedIn
	default:
		return ""
	}
}

func (h *handlers) draftSendFailed(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p tsubamev1.DraftSendFailed
	if !h.payload(env, &p) || !h.uuid(env, "draft id", p.GetDraftId()) {
		return nil
	}
	return h.record(ctx, tx, env, p.GetOwnerId(), func() (domain.Notice, error) {
		return domain.DraftSendFailed(p.GetDraftId(), p.GetReason())
	})
}

func (h *handlers) costThresholdReached(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p sorobanv1.CostThresholdReached
	if !h.payload(env, &p) {
		return nil
	}
	return h.record(ctx, tx, env, p.GetOwnerId(), func() (domain.Notice, error) {
		return domain.BudgetThreshold(scopeName(p.GetScopeType(), p.GetScopeValue()),
			enumName(p.GetPeriod(), "BUDGET_PERIOD_"), p.GetPercent())
	})
}

func (h *handlers) costBudgetExhausted(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope) error {
	var p sorobanv1.CostBudgetExhausted
	if !h.payload(env, &p) {
		return nil
	}
	return h.record(ctx, tx, env, p.GetOwnerId(), func() (domain.Notice, error) {
		return domain.BudgetExhausted(scopeName(p.GetScopeType(), p.GetScopeValue()),
			enumName(p.GetPeriod(), "BUDGET_PERIOD_"), p.GetResetsAt().AsTime())
	})
}

// record builds the notice and stores it for the owner. An event whose owner or
// content cannot be read is dropped, since a retry cannot help.
func (h *handlers) record(ctx context.Context, tx pgx.Tx, env *eventsv1.Envelope, ownerID string, build func() (domain.Notice, error)) error {
	owner, err := uuid.Parse(ownerID)
	if err != nil {
		return h.drop(env, errors.New("owner_id is not a uuid"))
	}
	eventID, err := uuid.Parse(env.GetId())
	if err != nil {
		return h.drop(env, errors.New("event id is not a uuid"))
	}
	notice, err := build()
	if err != nil {
		return h.drop(env, fmt.Errorf("build notification: %w", err))
	}
	return h.rec.Record(ctx, tx, app.RecordInput{OwnerID: owner, EventID: eventID, Notice: notice})
}

// payload decodes the envelope's payload into into. It reports false after
// dropping an event it cannot decode.
func (h *handlers) payload(env *eventsv1.Envelope, into proto.Message) bool {
	if err := env.GetPayload().UnmarshalTo(into); err != nil {
		_ = h.drop(env, fmt.Errorf("decode payload: %w", err))
		return false
	}
	return true
}

// uuid reports whether value is a uuid, dropping the event when it is not. Ids
// end up in links, so they must be well formed.
func (h *handlers) uuid(env *eventsv1.Envelope, what, value string) bool {
	if _, err := uuid.Parse(value); err != nil {
		_ = h.drop(env, fmt.Errorf("%s is not a uuid", what))
		return false
	}
	return true
}

// drop acknowledges an event that can never be handled, so its producer does
// not retry it for a day. It is logged as an error with ids only, never the
// payload.
func (h *handlers) drop(env *eventsv1.Envelope, reason error) error {
	h.log.Error("event dropped",
		slog.String("event_id", env.GetId()), slog.String("event_type", env.GetType()), slog.Any("reason", reason))
	return nil
}

// enumName turns a proto enum into its lower-case name without the prefix, such
// as DRAFT_KIND_COVER_LETTER into cover_letter.
func enumName(e fmt.Stringer, prefix string) string {
	return strings.ToLower(strings.TrimPrefix(e.String(), prefix))
}

// scopeName says what a budget covers in words for the notification.
func scopeName(scope sorobanv1.ScopeType, value string) string {
	if scope == sorobanv1.ScopeType_SCOPE_TYPE_GLOBAL || value == "" {
		return "overall"
	}
	return value
}
