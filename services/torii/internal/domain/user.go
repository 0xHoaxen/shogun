package domain

import (
	"time"

	"github.com/google/uuid"
)

// Role is what a user may do beyond their own data.
type Role string

// Roles. An admin manages users and approvals; neither role can see another
// user's content.
const (
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// Status says whether a user may sign in.
type Status string

// Statuses.
const (
	StatusActive   Status = "active"
	StatusDisabled Status = "disabled"
)

// DefaultTimezone is the zone a new user's days are counted in until they
// pick their own.
const DefaultTimezone = "Asia/Kolkata"

// User is one Google account. ID is the owner id used by every service.
type User struct {
	ID           uuid.UUID
	GoogleSub    string
	Email        string
	DisplayName  string
	Role         Role
	Status       Status
	GmailAllowed bool
	Timezone     string
	Version      int32
	CreatedAt    time.Time
	UpdatedAt    time.Time
}
