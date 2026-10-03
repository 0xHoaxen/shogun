package domain

import "fmt"

// Stable ErrorInfo.reason values for invalid state changes.
const (
	ReasonJobStatusInvalidTransition     = "JOB_STATUS_INVALID_TRANSITION"
	ReasonContactStatusInvalidTransition = "CONTACT_STATUS_INVALID_TRANSITION"
)

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
