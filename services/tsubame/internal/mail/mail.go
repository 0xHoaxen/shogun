// Package mail is how tsubame talks to a mail provider. Gmail is the only
// implementation; Provider is the seam an Outlook one would plug into.
package mail

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// DraftHeader marks mail tsubame sent for an approved draft. The reconciler
// looks for it in the Sent folder before it retries a send, so a draft is never
// sent twice.
const DraftHeader = "X-Shogun-Draft"

// Errors a provider returns. Use errors.Is.
var (
	// ErrAuthRevoked means the owner revoked access or the token no longer
	// works. The account needs to be connected again.
	ErrAuthRevoked = errors.New("mail: provider access revoked")
	// ErrHistoryExpired means the sync cursor is too old for an incremental
	// sync; do a full one.
	ErrHistoryExpired = errors.New("mail: history cursor expired")
	// ErrNotFound means the message is gone.
	ErrNotFound = errors.New("mail: message not found")
	// ErrInvalidMessage means an outgoing message cannot be turned into mail,
	// for example because a header value holds a line break.
	ErrInvalidMessage = errors.New("mail: invalid outgoing message")
)

// APIError is a provider answer that is not one of the sentinel errors. It
// carries the status and the provider's short message, never a body.
type APIError struct {
	Status  int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("mail: provider answered %d: %s", e.Status, e.Message)
}

// Direction is whether the account received or sent a message.
type Direction string

// The two directions, as in messages.direction.
const (
	Inbound  Direction = "inbound"
	Outbound Direction = "outbound"
)

// Profile identifies the connected mailbox.
type Profile struct {
	Address string
	// HistoryID is the provider's current cursor, to start syncing from.
	HistoryID string
}

// Message is the part of a mail tsubame keeps: headers and a snippet, never
// the full body.
type Message struct {
	ID         string
	ThreadID   string
	Direction  Direction
	From       string
	To         []string
	Subject    string
	Snippet    string
	ReceivedAt time.Time
	// DraftID is the value of DraftHeader, set on mail tsubame sent.
	DraftID string
}

// ListQuery selects messages. The Query syntax is the provider's own.
type ListQuery struct {
	Query     string
	PageToken string
	// Max is the page size; zero means the provider's default.
	Max int
}

// Page is one page of message ids.
type Page struct {
	IDs           []string
	NextPageToken string
}

// History is what changed since a cursor.
type History struct {
	// AddedIDs are messages that arrived or were sent since the cursor.
	AddedIDs []string
	// LatestID is the cursor to store after processing AddedIDs.
	LatestID string
}

// Outgoing is a message to send.
type Outgoing struct {
	From    string
	To      []string
	Subject string
	Body    string
	// DraftID is written to DraftHeader.
	DraftID string
}

// Sent is the provider's record of a sent message.
type Sent struct {
	ID       string
	ThreadID string
}

// Provider reads and sends mail for one connected account.
type Provider interface {
	// Profile returns the mailbox address and the current history cursor.
	Profile(ctx context.Context) (Profile, error)
	// List returns one page of message ids matching q.
	List(ctx context.Context, q ListQuery) (Page, error)
	// Get returns one message's headers and snippet.
	Get(ctx context.Context, id string) (Message, error)
	// History returns the messages added since the cursor.
	History(ctx context.Context, since string) (History, error)
	// Send sends a message exactly once per call.
	Send(ctx context.Context, m Outgoing) (Sent, error)
}

// Factory builds a Provider for an account from its refresh token.
type Factory interface {
	Provider(refreshToken string) Provider
}
