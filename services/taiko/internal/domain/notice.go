package domain

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Type says what happened. The set matches the CHECK on notifications.type.
type Type string

// Notification types.
const (
	TypeDraftReady      Type = "draft_ready"
	TypeDraftFailed     Type = "draft_failed"
	TypeDraftSendFailed Type = "draft_send_failed"
	TypeInterviewInvite Type = "interview_invite"
	TypeOffer           Type = "offer"
	TypeRejection       Type = "rejection"
	TypeReplyDetected   Type = "reply_detected"
	TypeFollowUpDue     Type = "follow_up_due"
	TypeBudgetThreshold Type = "budget_threshold"
	TypeBudgetExhausted Type = "budget_exhausted"
	TypeDailyDigest     Type = "daily_digest"
)

// Length limits for what a notification shows.
const (
	MaxTitleLength = 200
	MaxBodyLength  = 1000
	MaxLinkLength  = 300
)

// Errors NewNotice returns.
var (
	ErrInvalidType = errors.New("domain: unknown notification type")
	ErrEmptyTitle  = errors.New("domain: notification title is empty")
	ErrTooLong     = errors.New("domain: notification text is too long")
	ErrInvalidLink = errors.New("domain: notification link is not an in-app route")
)

var knownTypes = map[Type]struct{}{
	TypeDraftReady: {}, TypeDraftFailed: {}, TypeDraftSendFailed: {}, TypeInterviewInvite: {},
	TypeOffer: {}, TypeRejection: {}, TypeReplyDetected: {}, TypeFollowUpDue: {},
	TypeBudgetThreshold: {}, TypeBudgetExhausted: {}, TypeDailyDigest: {},
}

// Valid reports whether t is a known notification type.
func (t Type) Valid() bool {
	_, ok := knownTypes[t]
	return ok
}

// Notice is the content of a notification, before it is stored. It never
// holds message bodies or email addresses, only what the owner needs to see
// and an in-app route to follow.
type Notice struct {
	Type  Type
	Title string
	Body  string
	// Link is an in-app route such as /drafts/<id>, or empty.
	Link string
}

// NewNotice validates and returns a Notice. Title and body are trimmed.
func NewNotice(t Type, title, body, link string) (Notice, error) {
	title, body = strings.TrimSpace(title), strings.TrimSpace(body)
	switch {
	case !t.Valid():
		return Notice{}, fmt.Errorf("%w: %q", ErrInvalidType, t)
	case title == "":
		return Notice{}, ErrEmptyTitle
	case utf8.RuneCountInString(title) > MaxTitleLength:
		return Notice{}, fmt.Errorf("%w: title over %d characters", ErrTooLong, MaxTitleLength)
	case utf8.RuneCountInString(body) > MaxBodyLength:
		return Notice{}, fmt.Errorf("%w: body over %d characters", ErrTooLong, MaxBodyLength)
	case !validLink(link):
		return Notice{}, fmt.Errorf("%w: %q", ErrInvalidLink, link)
	}
	return Notice{Type: t, Title: title, Body: body, Link: link}, nil
}

// validLink accepts an empty link or a path on this app. A scheme or a
// protocol-relative "//host" would send the owner elsewhere.
func validLink(link string) bool {
	if link == "" {
		return true
	}
	if len(link) > MaxLinkLength || !strings.HasPrefix(link, "/") || strings.HasPrefix(link, "//") {
		return false
	}
	return !strings.ContainsAny(link, "\\\r\n")
}
