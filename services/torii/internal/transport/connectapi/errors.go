package connectapi

import (
	"errors"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
)

// errorDomain is the ErrorInfo.domain of every error torii returns.
const errorDomain = "shogun"

// Stable ErrorInfo.reason values the web app switches on.
const (
	reasonUnauthenticated = "UNAUTHENTICATED"
	reasonInternal        = "INTERNAL"
)

// newError returns a Connect error with a stable ErrorInfo.reason, so clients
// never have to parse the message.
func newError(code connect.Code, reason, message string) *connect.Error {
	err := connect.NewError(code, errors.New(message))
	if detail, detailErr := connect.NewErrorDetail(&errdetails.ErrorInfo{Reason: reason, Domain: errorDomain}); detailErr == nil {
		err.AddDetail(detail)
	}
	return err
}
