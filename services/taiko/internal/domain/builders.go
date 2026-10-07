package domain

import (
	"errors"
	"fmt"
	"time"
)

// In-app routes a notification can link to.
const (
	draftsRoute   = "/drafts/"
	jobsRoute     = "/jobs"
	contactsRoute = "/contacts"
	spendRoute    = "/settings/spend"
	profileRoute  = "/profile"
)

// Follow-up targets.
const (
	TargetJob     = "job"
	TargetContact = "contact"
)

// ErrEmptyDigest is returned when a digest has nothing to say.
var ErrEmptyDigest = errors.New("domain: digest is empty")

// ErrInvalidTarget is returned for a follow-up target that is neither a job nor
// a contact.
var ErrInvalidTarget = errors.New("domain: unknown follow-up target")

var kindLabels = map[string]string{
	"cover_letter": "Cover letter",
	"outreach":     "Outreach message",
	"follow_up":    "Follow-up",
	"post":         "Post",
	"one_off":      "Message",
}

var mailTitles = map[Type]string{
	TypeInterviewInvite: "Interview invite received",
	TypeOffer:           "Offer received",
	TypeRejection:       "Application rejected",
}

func kindLabel(kind string) string {
	if label, ok := kindLabels[kind]; ok {
		return label
	}
	return "Draft"
}

// DraftReady announces a generated draft waiting for review. kind is a draft
// kind such as cover_letter.
func DraftReady(draftID, kind string) (Notice, error) {
	return NewNotice(TypeDraftReady, kindLabel(kind)+" ready", "Review and approve it in the queue.", draftsRoute+draftID)
}

// DraftFailed announces a draft that could not be generated. reason is a short
// code, never text from a message.
func DraftFailed(draftID, reason string) (Notice, error) {
	return NewNotice(TypeDraftFailed, "Draft generation failed", "Reason: "+reason, draftsRoute+draftID)
}

// DraftSendFailed announces an approved email that did not go out.
func DraftSendFailed(draftID, reason string) (Notice, error) {
	return NewNotice(TypeDraftSendFailed, "Email was not sent",
		"Reason: "+reason+". Review the draft and approve it again.", draftsRoute+draftID)
}

// Mail announces a classified message. t must be an interview invite, an offer
// or a rejection.
func Mail(t Type) (Notice, error) {
	title, ok := mailTitles[t]
	if !ok {
		return Notice{}, fmt.Errorf("%w: %q is not a mail notification", ErrInvalidType, t)
	}
	return NewNotice(t, title, "Open the job board to see the application.", jobsRoute)
}

// ReplyDetected announces that a contact wrote back.
func ReplyDetected() (Notice, error) {
	return NewNotice(TypeReplyDetected, "A contact replied", "Check the contact's timeline.", contactsRoute)
}

// FollowUpDue announces a follow-up that is due on dueOn (YYYY-MM-DD). target
// is TargetJob or TargetContact.
func FollowUpDue(target, dueOn string) (Notice, error) {
	var title, link string
	switch target {
	case TargetJob:
		title, link = "Job follow-up due", jobsRoute
	case TargetContact:
		title, link = "Contact follow-up due", contactsRoute
	default:
		return Notice{}, fmt.Errorf("%w: %q", ErrInvalidTarget, target)
	}
	return NewNotice(TypeFollowUpDue, title, "Due on "+dueOn+".", link)
}

// BudgetThreshold announces that spend crossed percent of a budget. scope names
// what the budget covers, such as overall or a service.
func BudgetThreshold(scope, period string, percent int32) (Notice, error) {
	return NewNotice(TypeBudgetThreshold,
		fmt.Sprintf("Claude spend at %d%% of the %s budget", percent, period), "Scope: "+scope+".", spendRoute)
}

// BudgetExhausted announces that a budget is used up until resetsAt.
func BudgetExhausted(scope, period string, resetsAt time.Time) (Notice, error) {
	body := fmt.Sprintf("The %s budget for %s is used up. Calls resume %s.",
		period, scope, resetsAt.UTC().Format("2006-01-02 15:04 UTC"))
	return NewNotice(TypeBudgetExhausted, "Claude budget used up", body, spendRoute)
}

// Suggestion targets, as katana names them.
const (
	SuggestionResume   = "resume"
	SuggestionLinkedIn = "linkedin"
)

// ProfileSuggestion announces a suggested edit to the owner's resume or
// LinkedIn profile. target is SuggestionResume or SuggestionLinkedIn; anything
// else is announced without naming the document.
func ProfileSuggestion(target string) (Notice, error) {
	title := "New profile suggestion"
	switch target {
	case SuggestionResume:
		title = "New resume suggestion"
	case SuggestionLinkedIn:
		title = "New LinkedIn suggestion"
	}
	return NewNotice(TypeProfileSuggestion, title, "Review it and accept or dismiss it.", profileRoute)
}
