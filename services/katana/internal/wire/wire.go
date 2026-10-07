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
