package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/torii/internal/domain"
)

// SetTimezone changes the zone a user's days are counted in. Their follow-up
// scans, digests and rollups in the other services follow it, through the
// user.updated event the store writes with the change.
func (a *Auth) SetTimezone(ctx context.Context, owner uuid.UUID, name string) (domain.User, error) {
	name = strings.TrimSpace(name)
	// "Local" and "UTC" abbreviations are not IANA place names; an empty name
	// would mean UTC to time.LoadLocation, which is not what the user chose.
	if name == "" || name == "Local" {
		return domain.User{}, ErrInvalidTimezone
	}
	if _, err := time.LoadLocation(name); err != nil {
		return domain.User{}, ErrInvalidTimezone
	}
	user, err := a.users.SetTimezone(ctx, owner, name)
	if err != nil {
		return domain.User{}, fmt.Errorf("set timezone: %w", err)
	}
	return user, nil
}

// EndAllSessions signs the user out on every device, this one included, and
// returns how many sessions it ended.
func (a *Auth) EndAllSessions(ctx context.Context, owner uuid.UUID) (int64, error) {
	n, err := a.store.DeleteByOwner(ctx, owner)
	if err != nil {
		return 0, fmt.Errorf("end all sessions: %w", err)
	}
	return n, nil
}

// DeleteAccount deletes the user's account after they type their email to
// confirm. It ends every session, removes the email and name, and announces
// user.deleted so each service purges the user's data. It returns how many
// sessions it ended.
func (a *Auth) DeleteAccount(ctx context.Context, owner uuid.UUID, typedEmail string) (int64, error) {
	user, err := a.users.ByID(ctx, owner)
	if err != nil {
		return 0, fmt.Errorf("find account: %w", err)
	}
	if typed := normaliseEmail(typedEmail); typed == "" || typed != normaliseEmail(user.Email) {
		return 0, ErrConfirmationMismatch
	}
	// Sessions first: if the delete then fails, the user is only signed out and can try again.
	ended, err := a.store.DeleteByOwner(ctx, owner)
	if err != nil {
		return 0, fmt.Errorf("end sessions: %w", err)
	}
	if _, err := a.users.Archive(ctx, owner); err != nil {
		return 0, fmt.Errorf("delete account: %w", err)
	}
	return ended, nil
}
