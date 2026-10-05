package llm

import (
	"fmt"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BudgetError is the refusal of a hard budget. It satisfies
// errors.Is(err, ErrBudgetExhausted).
type BudgetError struct {
	BudgetID string
	// ResetsAt is when the budget's period ends and calls are allowed again;
	// zero if soroban did not say.
	ResetsAt time.Time
	cause    error
}

func (e *BudgetError) Error() string {
	if e.ResetsAt.IsZero() {
		return "llm: budget exhausted"
	}
	return fmt.Sprintf("llm: budget exhausted until %s", e.ResetsAt.Format(time.RFC3339))
}

// Is reports whether target is ErrBudgetExhausted.
func (e *BudgetError) Is(target error) bool { return target == ErrBudgetExhausted }

// Unwrap returns the status error soroban answered with.
func (e *BudgetError) Unwrap() error { return e.cause }

// reserveError turns a failed Reserve into a *BudgetError for a refusal and
// ErrMeteringUnavailable for anything else, so that no failure lets a call
// through.
func reserveError(err error) error {
	st, ok := status.FromError(err)
	if !ok || st.Code() != codes.ResourceExhausted {
		return fmt.Errorf("%w: %w", ErrMeteringUnavailable, err)
	}
	refusal := &BudgetError{cause: err}
	for _, d := range st.Details() {
		info, ok := d.(*errdetails.ErrorInfo)
		if !ok {
			continue
		}
		refusal.BudgetID = info.GetMetadata()["budget_id"]
		if t, perr := time.Parse(time.RFC3339, info.GetMetadata()["resets_at"]); perr == nil {
			refusal.ResetsAt = t
		}
	}
	return refusal
}
