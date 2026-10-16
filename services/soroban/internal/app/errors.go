package app

import (
	"errors"
	"fmt"
)

// ErrNoOwner means the call carries no identity, or one whose owner is not a
// UUID. Transport maps it to PermissionDenied.
var ErrNoOwner = errors.New("app: call has no valid owner")

// ErrReservationNotOpen means usage cannot be recorded against a reservation
// because it was released. Transport maps it to FailedPrecondition.
var ErrReservationNotOpen = errors.New("app: reservation is not open")

// Reasons of the InvalidArgumentError a use case returns.
const (
	ReasonInvalidRequest  = "INVALID_REQUEST"
	ReasonInvalidTokens   = "INVALID_TOKEN_COUNT"
	ReasonFeatureMismatch = "FEATURE_SERVICE_MISMATCH"
	ReasonModelUnpriced   = "MODEL_PRICE_UNKNOWN"
)

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

// Errors about a user's AI provider. Transport maps both to FailedPrecondition.
var (
	// ErrProviderStorageOff means soroban has no master key to seal API keys, so
	// providers cannot be stored or opened.
	ErrProviderStorageOff = errors.New("app: provider storage is not configured")
	// ErrProviderNotConfigured means the caller has not set an AI provider, so
	// their AI features are off.
	ErrProviderNotConfigured = errors.New("app: no AI provider is configured")
)

// Reasons of the InvalidArgumentError the provider use cases return.
const (
	ReasonInvalidProviderURL   = "INVALID_PROVIDER_URL"
	ReasonInvalidProviderModel = "INVALID_PROVIDER_MODEL"
	ReasonInvalidAPIKey        = "INVALID_API_KEY"
	ReasonAPIKeyRequired       = "API_KEY_REQUIRED"
	ReasonInvalidTokenCap      = "INVALID_TOKEN_CAP"
)

// Reasons of the InvalidArgumentError the admin use cases return.
const (
	ReasonInvalidBudget = "INVALID_BUDGET"
	ReasonInvalidRange  = "INVALID_RANGE"
	ReasonInvalidPrice  = "INVALID_PRICE"
)
