package app

import (
	"errors"
	"fmt"
)

// Errors the transport maps to gRPC status codes.
var (
	// ErrNoOwner means the call carries no identity, or one whose owner is not
	// a UUID.
	ErrNoOwner = errors.New("app: call has no valid owner")
	// ErrSourceNotFound means the owner has no such source.
	ErrSourceNotFound = errors.New("app: source not found")
	// ErrKindFixed means an update tried to change what a source reads.
	ErrKindFixed = errors.New("app: a source's kind cannot change")
	// ErrTooManySources means the owner has the most sources allowed.
	ErrTooManySources = errors.New("app: too many sources")
)

// RunError means a source could not be read. Code is a short stable word that is
// kept with the source as its last error and shown to the owner.
type RunError struct {
	Code string
	Err  error
}

func (e *RunError) Error() string { return fmt.Sprintf("source run failed (%s): %v", e.Code, e.Err) }

// Unwrap returns the cause.
func (e *RunError) Unwrap() error { return e.Err }

func wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s: %w", op, err)
}
