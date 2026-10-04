package domain

import (
	"time"

	"github.com/google/uuid"
)

// Session is a signed-in owner's browser session.
type Session struct {
	ID          uuid.UUID
	OwnerID     uuid.UUID
	Email       string
	DisplayName string
	ExpiresAt   time.Time
}

// Expired reports whether the session is no longer valid at now.
func (s Session) Expired(now time.Time) bool {
	return !now.Before(s.ExpiresAt)
}

// Renewed returns a copy of the session pushed out to now+ttl once less than
// half of ttl is left, so an active owner stays signed in. The bool is false,
// and the session unchanged, when no renewal is due.
func (s Session) Renewed(now time.Time, ttl time.Duration) (Session, bool) {
	if s.ExpiresAt.Sub(now) >= ttl/2 {
		return s, false
	}
	renewed := s
	renewed.ExpiresAt = now.Add(ttl)
	return renewed, true
}
