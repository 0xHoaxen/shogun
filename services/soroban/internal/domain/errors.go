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

// ReasonTokenCapExhausted is the stable ErrorInfo.reason of a Reserve refused by
// the user's monthly token cap.
const ReasonTokenCapExhausted = "TOKEN_CAP_EXHAUSTED"

// TokenCapExhaustedError reports that a call would pass the user's monthly token
// cap. Unlike a budget there is no event: the user chose the cap and sees it.
type TokenCapExhaustedError struct {
	Cap      int64
	Used     int64
	ResetsAt time.Time
}

func (e *TokenCapExhaustedError) Error() string {
	return fmt.Sprintf("%s: %d of %d tokens used this month, until %s",
		ReasonTokenCapExhausted, e.Used, e.Cap, e.ResetsAt.Format(time.RFC3339))
}
