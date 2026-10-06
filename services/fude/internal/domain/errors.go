package domain

import "fmt"

// ReasonDraftStateInvalidTransition is the stable ErrorInfo.reason for an
// invalid draft state change.
const ReasonDraftStateInvalidTransition = "DRAFT_STATE_INVALID_TRANSITION"

// ReasonDraftChannelNotCopyOnly is the stable ErrorInfo.reason for marking an
// email draft as posted. An email becomes sent only when tsubame reports it.
const ReasonDraftChannelNotCopyOnly = "DRAFT_CHANNEL_NOT_COPY_ONLY"

// TransitionError reports a state move the draft state machine does not
// allow. Transport maps it to FailedPrecondition carrying Reason.
type TransitionError struct {
	Reason string
	From   string
	To     string
}

func (e *TransitionError) Error() string {
	return fmt.Sprintf("%s: %s -> %s", e.Reason, e.From, e.To)
}
