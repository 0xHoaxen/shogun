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

const modePrefix = "BUDGET_MODE_"

// ModeToProto returns the enum for m, or unspecified for an unknown mode.
func ModeToProto(m domain.Mode) sorobanv1.BudgetMode {
	return sorobanv1.BudgetMode(sorobanv1.BudgetMode_value[modePrefix+strings.ToUpper(string(m))])
}

// ScopeTypeFromProto returns the domain scope for p. It reports false for
// unspecified or unknown enum values.
func ScopeTypeFromProto(p sorobanv1.ScopeType) (domain.ScopeType, bool) {
	s, ok := fromProto(p.String(), scopePrefix, p == sorobanv1.ScopeType_SCOPE_TYPE_UNSPECIFIED)
	return domain.ScopeType(s), ok && domain.ScopeType(s).Valid()
}

// PeriodFromProto returns the domain period for p. It reports false for
// unspecified or unknown enum values.
func PeriodFromProto(p sorobanv1.BudgetPeriod) (domain.Period, bool) {
	s, ok := fromProto(p.String(), periodPrefix, p == sorobanv1.BudgetPeriod_BUDGET_PERIOD_UNSPECIFIED)
	return domain.Period(s), ok && domain.Period(s).Valid()
}

// ModeFromProto returns the domain mode for p. It reports false for
// unspecified or unknown enum values.
func ModeFromProto(p sorobanv1.BudgetMode) (domain.Mode, bool) {
	s, ok := fromProto(p.String(), modePrefix, p == sorobanv1.BudgetMode_BUDGET_MODE_UNSPECIFIED)
	return domain.Mode(s), ok && domain.Mode(s).Valid()
}

// SpendGroupFromProto returns the grouping name (service, feature, model or
// day) for p. It reports false for unspecified or unknown enum values.
func SpendGroupFromProto(p sorobanv1.SpendGroup) (string, bool) {
	return fromProto(p.String(), "SPEND_GROUP_", p == sorobanv1.SpendGroup_SPEND_GROUP_UNSPECIFIED)
}

// fromProto strips prefix from an enum name and lower-cases the rest.
func fromProto(name, prefix string, unspecified bool) (string, bool) {
	rest, ok := strings.CutPrefix(name, prefix)
	if !ok || unspecified {
		return "", false
	}
	return strings.ToLower(rest), true
}
