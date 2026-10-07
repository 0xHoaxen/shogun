package domain

import (
	"errors"
	"fmt"
)

// ReasonItemStatusInvalidTransition is the stable ErrorInfo.reason for a status
// move the state machine does not allow.
const ReasonItemStatusInvalidTransition = "ITEM_STATUS_INVALID_TRANSITION"

// ErrInvalid marks input that breaks a domain rule. Transport maps it to
// InvalidArgument.
var ErrInvalid = errors.New("invalid input")

// TransitionError reports a status move the state machine does not allow.
// Transport maps it to FailedPrecondition carrying Reason.
type TransitionError struct {
	Reason string
	From   string
	To     string
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("%s: %s -> %s", e.Reason, e.From, e.To)
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}
