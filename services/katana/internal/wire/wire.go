// Package wire converts between katana's domain enums and the generated proto
// enums.
package wire

import (
	katanav1 "github.com/0xHoaxen/shogun/gen/go/shogun/katana/v1"
	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
)

// TargetToProto returns the proto target, or unspecified for an unknown one.
func TargetToProto(t domain.Target) katanav1.SuggestionTarget {
	switch t {
	case domain.TargetResume:
		return katanav1.SuggestionTarget_SUGGESTION_TARGET_RESUME
	case domain.TargetLinkedIn:
		return katanav1.SuggestionTarget_SUGGESTION_TARGET_LINKEDIN
	default:
		return katanav1.SuggestionTarget_SUGGESTION_TARGET_UNSPECIFIED
	}
}

// TargetFromProto returns the domain target, or "" for unspecified.
func TargetFromProto(t katanav1.SuggestionTarget) domain.Target {
	switch t {
	case katanav1.SuggestionTarget_SUGGESTION_TARGET_RESUME:
		return domain.TargetResume
	case katanav1.SuggestionTarget_SUGGESTION_TARGET_LINKEDIN:
		return domain.TargetLinkedIn
	default:
		return ""
	}
}

// StateToProto returns the proto state, or unspecified for an unknown one.
func StateToProto(s domain.State) katanav1.SuggestionState {
	switch s {
	case domain.StateOpen:
		return katanav1.SuggestionState_SUGGESTION_STATE_OPEN
	case domain.StateAccepted:
		return katanav1.SuggestionState_SUGGESTION_STATE_ACCEPTED
	case domain.StateDismissed:
		return katanav1.SuggestionState_SUGGESTION_STATE_DISMISSED
	default:
		return katanav1.SuggestionState_SUGGESTION_STATE_UNSPECIFIED
	}
}

// StateFromProto returns the domain state, or "" for unspecified.
func StateFromProto(s katanav1.SuggestionState) domain.State {
	switch s {
	case katanav1.SuggestionState_SUGGESTION_STATE_OPEN:
		return domain.StateOpen
	case katanav1.SuggestionState_SUGGESTION_STATE_ACCEPTED:
		return domain.StateAccepted
	case katanav1.SuggestionState_SUGGESTION_STATE_DISMISSED:
		return domain.StateDismissed
	default:
		return ""
	}
}
