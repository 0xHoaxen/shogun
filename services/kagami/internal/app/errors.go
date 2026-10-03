package app

import (
	"errors"
	"fmt"
)

// ErrNoOwner means the call carries no identity, or one whose owner is not a
// UUID. Transport maps it to PermissionDenied.
var ErrNoOwner = errors.New("app: call has no valid owner")

// InvalidArgumentError reports input a use case refuses. Reason is a stable
// upper-case code for clients.
type InvalidArgumentError struct {
	Reason string
	Msg    string
}

func (e *InvalidArgumentError) Error() string {
	return fmt.Sprintf("%s: %s", e.Reason, e.Msg)
}

func invalid(reason, format string, args ...any) error {
	return &InvalidArgumentError{Reason: reason, Msg: fmt.Sprintf(format, args...)}
}
