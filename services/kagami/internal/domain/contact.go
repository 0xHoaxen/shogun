package domain

import "time"

// ContactStatus is where outreach to a person stands. Values match the
// contacts.status CHECK.
type ContactStatus string

// The outreach stages for a contact.
const (
	ContactNotReached          ContactStatus = "not_reached"
	ContactReachedOut          ContactStatus = "reached_out"
	ContactConversationStarted ContactStatus = "conversation_started"
	ContactReplied             ContactStatus = "replied"
	ContactReferralAsked       ContactStatus = "referral_asked"
)

// contactTransitions follows docs/shogun-state-machines.md.
var contactTransitions = transitions[ContactStatus]{
	ContactNotReached:          {ContactReachedOut},
	ContactReachedOut:          {ContactConversationStarted, ContactReplied},
	ContactReplied:             {ContactConversationStarted, ContactReferralAsked},
	ContactConversationStarted: {ContactReferralAsked, ContactReplied},
	ContactReferralAsked:       {ContactReplied, ContactConversationStarted},
}

// Valid reports whether s is a known contact status.
func (s ContactStatus) Valid() bool {
	_, ok := contactTransitions[s]
	return ok
}

// CanMoveTo reports whether a contact in status s may move to next.
func (s ContactStatus) CanMoveTo(next ContactStatus) bool {
	return contactTransitions.allows(s, next)
}

// Contact is a person in the contact book. Methods return a changed copy.
type Contact struct {
	ID            string
	FullName      string
	Status        ContactStatus
	LastContacted *time.Time
	NextFollowUp  *time.Time
	Version       int32
	UpdatedAt     time.Time
}

// ChangeStatus returns the contact moved to next, or a *TransitionError.
func (c Contact) ChangeStatus(next ContactStatus, now time.Time) (Contact, error) {
	if !c.Status.CanMoveTo(next) {
		return c, &TransitionError{
			Reason: ReasonContactStatusInvalidTransition,
			From:   string(c.Status),
			To:     string(next),
		}
	}
	c.Status = next
	c.UpdatedAt = now
	return c, nil
}
