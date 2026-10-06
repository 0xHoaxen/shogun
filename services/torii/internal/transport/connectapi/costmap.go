package connectapi

import (
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
)

const reasonInvalidBudget = "INVALID_BUDGET"

func timestampOf(t time.Time) *timestamppb.Timestamp { return timestamppb.New(t) }

func spendGroupToSoroban(g apiv1.SpendGroup) (sorobanv1.SpendGroup, bool) {
	switch g {
	case apiv1.SpendGroup_SPEND_GROUP_DAY:
		return sorobanv1.SpendGroup_SPEND_GROUP_DAY, true
	case apiv1.SpendGroup_SPEND_GROUP_SERVICE:
		return sorobanv1.SpendGroup_SPEND_GROUP_SERVICE, true
	case apiv1.SpendGroup_SPEND_GROUP_FEATURE:
		return sorobanv1.SpendGroup_SPEND_GROUP_FEATURE, true
	case apiv1.SpendGroup_SPEND_GROUP_MODEL:
		return sorobanv1.SpendGroup_SPEND_GROUP_MODEL, true
	case apiv1.SpendGroup_SPEND_GROUP_UNSPECIFIED:
		return sorobanv1.SpendGroup_SPEND_GROUP_UNSPECIFIED, false
	}
	return sorobanv1.SpendGroup_SPEND_GROUP_UNSPECIFIED, false
}

func budgetModeToSoroban(m apiv1.BudgetMode) sorobanv1.BudgetMode {
	switch m {
	case apiv1.BudgetMode_BUDGET_MODE_HARD:
		return sorobanv1.BudgetMode_BUDGET_MODE_HARD
	case apiv1.BudgetMode_BUDGET_MODE_SOFT:
		return sorobanv1.BudgetMode_BUDGET_MODE_SOFT
	case apiv1.BudgetMode_BUDGET_MODE_UNSPECIFIED:
		return sorobanv1.BudgetMode_BUDGET_MODE_UNSPECIFIED
	}
	return sorobanv1.BudgetMode_BUDGET_MODE_UNSPECIFIED
}

func budgetModeToAPI(m sorobanv1.BudgetMode) apiv1.BudgetMode {
	switch m {
	case sorobanv1.BudgetMode_BUDGET_MODE_HARD:
		return apiv1.BudgetMode_BUDGET_MODE_HARD
	case sorobanv1.BudgetMode_BUDGET_MODE_SOFT:
		return apiv1.BudgetMode_BUDGET_MODE_SOFT
	case sorobanv1.BudgetMode_BUDGET_MODE_UNSPECIFIED:
		return apiv1.BudgetMode_BUDGET_MODE_UNSPECIFIED
	}
	return apiv1.BudgetMode_BUDGET_MODE_UNSPECIFIED
}

func budgetScopeToAPI(s sorobanv1.ScopeType) apiv1.BudgetScope {
	switch s {
	case sorobanv1.ScopeType_SCOPE_TYPE_GLOBAL:
		return apiv1.BudgetScope_BUDGET_SCOPE_GLOBAL
	case sorobanv1.ScopeType_SCOPE_TYPE_SERVICE:
		return apiv1.BudgetScope_BUDGET_SCOPE_SERVICE
	case sorobanv1.ScopeType_SCOPE_TYPE_FEATURE:
		return apiv1.BudgetScope_BUDGET_SCOPE_FEATURE
	case sorobanv1.ScopeType_SCOPE_TYPE_UNSPECIFIED:
		return apiv1.BudgetScope_BUDGET_SCOPE_UNSPECIFIED
	}
	return apiv1.BudgetScope_BUDGET_SCOPE_UNSPECIFIED
}

func budgetPeriodToAPI(p sorobanv1.BudgetPeriod) apiv1.BudgetPeriod {
	switch p {
	case sorobanv1.BudgetPeriod_BUDGET_PERIOD_DAILY:
		return apiv1.BudgetPeriod_BUDGET_PERIOD_DAILY
	case sorobanv1.BudgetPeriod_BUDGET_PERIOD_MONTHLY:
		return apiv1.BudgetPeriod_BUDGET_PERIOD_MONTHLY
	case sorobanv1.BudgetPeriod_BUDGET_PERIOD_UNSPECIFIED:
		return apiv1.BudgetPeriod_BUDGET_PERIOD_UNSPECIFIED
	}
	return apiv1.BudgetPeriod_BUDGET_PERIOD_UNSPECIFIED
}

func budgetToAPI(b *sorobanv1.Budget) *apiv1.Budget {
	return &apiv1.Budget{
		Id:             b.GetId(),
		Scope:          budgetScopeToAPI(b.GetScopeType()),
		ScopeValue:     b.GetScopeValue(),
		Period:         budgetPeriodToAPI(b.GetPeriod()),
		LimitMicros:    b.GetLimitMicros(),
		Mode:           budgetModeToAPI(b.GetMode()),
		Thresholds:     b.GetThresholds(),
		Enabled:        b.GetEnabled(),
		Version:        b.GetVersion(),
		SpentMicros:    b.GetSpentMicros(),
		ReservedMicros: b.GetReservedMicros(),
		ResetsAt:       b.GetResetsAt().AsTime().UTC().Format(time.RFC3339),
	}
}

func spendToAPI(r *sorobanv1.GetSpendResponse) *apiv1.GetSpendResponse {
	rows := make([]*apiv1.SpendRow, 0, len(r.GetRows()))
	for _, row := range r.GetRows() {
		u := row.GetUsage()
		rows = append(rows, &apiv1.SpendRow{
			Key:              row.GetKey(),
			CostMicros:       row.GetCostMicros(),
			InputTokens:      int64(u.GetInputTokens()),
			OutputTokens:     int64(u.GetOutputTokens()),
			CacheReadTokens:  int64(u.GetCacheReadTokens()),
			CacheWriteTokens: int64(u.GetCacheWriteTokens()),
		})
	}
	return &apiv1.GetSpendResponse{Rows: rows, TotalMicros: r.GetTotalMicros()}
}
