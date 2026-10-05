package domain

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ReasonBudgetExhausted is the stable ErrorInfo.reason of a refused Reserve.
const ReasonBudgetExhausted = "BUDGET_EXHAUSTED"

// BudgetExhaustedError reports the hard budget that blocked a reservation.
type BudgetExhaustedError struct {
	BudgetID   uuid.UUID
	ScopeType  ScopeType
	ScopeValue string
	Period     Period
	ResetsAt   time.Time
}

func (e *BudgetExhaustedError) Error() string {
	scope := string(e.ScopeType)
	if e.ScopeValue != "" {
		scope += " " + e.ScopeValue
	}
	return fmt.Sprintf("%s: %s %s budget is used up until %s",
		ReasonBudgetExhausted, scope, e.Period, e.ResetsAt.Format(time.RFC3339))
}
