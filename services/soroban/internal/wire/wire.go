// Package wire converts between domain values and the soroban.v1 proto enums.
package wire

import (
	"strings"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/services/soroban/internal/domain"
)

const (
	scopePrefix  = "SCOPE_TYPE_"
	periodPrefix = "BUDGET_PERIOD_"
)

// ScopeTypeToProto returns the enum for s, or unspecified for an unknown scope.
func ScopeTypeToProto(s domain.ScopeType) sorobanv1.ScopeType {
	return sorobanv1.ScopeType(sorobanv1.ScopeType_value[scopePrefix+strings.ToUpper(string(s))])
}

// PeriodToProto returns the enum for p, or unspecified for an unknown period.
func PeriodToProto(p domain.Period) sorobanv1.BudgetPeriod {
	return sorobanv1.BudgetPeriod(sorobanv1.BudgetPeriod_value[periodPrefix+strings.ToUpper(string(p))])
}
