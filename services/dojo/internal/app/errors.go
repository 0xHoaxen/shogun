package app

import (
	"errors"
	"fmt"

	"github.com/0xHoaxen/shogun/services/dojo/internal/domain"
	"github.com/0xHoaxen/shogun/services/dojo/internal/store"
)

// Errors the transport maps to gRPC status codes.
var (
	// ErrNoOwner means the call carries no identity, or one whose owner is not
	// a UUID.
	ErrNoOwner = errors.New("app: call has no valid owner")
	// ErrItemNotFound means the owner has no such item.
	ErrItemNotFound = errors.New("app: item not found")
	// ErrActivityNotFound means the owner has no such activity.
	ErrActivityNotFound = errors.New("app: activity not found")
	// ErrDraftsUnavailable means fude could not be reached or refused the call
	// for a reason the owner cannot fix.
	ErrDraftsUnavailable = errors.New("app: drafts are unavailable")
)

// notFound turns the store's not-found error into the given app error and
// leaves any other error alone.
func notFound(err, target error) error {
	if errors.Is(err, store.ErrNotFound) {
		return target
	}
	return err
}

func wrapOp(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}

func isNotFound(err error) bool { return errors.Is(err, store.ErrNotFound) }

// invalidInput returns an error wrapping domain.ErrInvalid, for rules that
// belong to a use case rather than to an entity.
func invalidInput(msg string) error {
	return fmt.Errorf("%w: %s", domain.ErrInvalid, msg)
}
