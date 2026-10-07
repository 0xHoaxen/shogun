package app

import (
	"errors"
	"fmt"
)

// ErrNoOwner means the call carries no identity, or one whose owner is not a
// UUID. Transport maps it to PermissionDenied.
var ErrNoOwner = errors.New("app: call has no valid owner")

// ErrStreamReset means a stream was closed because it fell behind or because
// the live feed was interrupted. The client reconnects with the last id it saw
// and nothing is lost. Transport maps it to Unavailable.
var ErrStreamReset = errors.New("app: stream reset, reconnect with the last seen id")

// InvalidArgumentError reports input a use case refuses. Reason is a stable
// upper-case code for clients.
type InvalidArgumentError struct {
	Reason string
	Msg    string
}

func (e *InvalidArgumentError) Error() string {
	return fmt.Sprintf("%s: %s", e.Reason, e.Msg)
}
