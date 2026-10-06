// Package wire converts between domain values and the fude.v1 proto enums.
// Domain values are the lower-case enum names without their prefix.
package wire

import (
	"strings"

	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
)

const (
	kindPrefix   = "DRAFT_KIND_"
	targetPrefix = "TARGET_TYPE_"
	channelPref  = "CHANNEL_"
	statePrefix  = "DRAFT_STATE_"
	authorPrefix = "VERSION_AUTHOR_"
)

type protoEnum interface {
	~int32
	String() string
}

func toProto[P protoEnum](prefix string, values map[string]int32, s string) P {
	return P(values[prefix+strings.ToUpper(s)])
}

// fromProto returns the domain value for p, or false for unspecified.
func fromProto[P protoEnum](prefix string, p P) (string, bool) {
	name, ok := strings.CutPrefix(p.String(), prefix)
	if !ok || p == 0 {
		return "", false
	}
	return strings.ToLower(name), true
}

// KindToProto returns the enum for k, or unspecified for an unknown kind.
func KindToProto(k domain.Kind) fudev1.DraftKind {
	return toProto[fudev1.DraftKind](kindPrefix, fudev1.DraftKind_value, string(k))
}

// KindFromProto returns the domain kind for p. It reports false for
// unspecified or unknown enum values.
func KindFromProto(p fudev1.DraftKind) (domain.Kind, bool) {
	s, ok := fromProto(kindPrefix, p)
	k := domain.Kind(s)
	return k, ok && k.Valid()
}

// TargetTypeToProto returns the enum for t.
func TargetTypeToProto(t domain.TargetType) fudev1.TargetType {
	return toProto[fudev1.TargetType](targetPrefix, fudev1.TargetType_value, string(t))
}

// TargetTypeFromProto returns the domain target type for p.
func TargetTypeFromProto(p fudev1.TargetType) (domain.TargetType, bool) {
	s, ok := fromProto(targetPrefix, p)
	t := domain.TargetType(s)
	return t, ok && t.Valid()
}

// ChannelToProto returns the enum for c.
func ChannelToProto(c domain.Channel) fudev1.Channel {
	return toProto[fudev1.Channel](channelPref, fudev1.Channel_value, string(c))
}

// ChannelFromProto returns the domain channel for p.
func ChannelFromProto(p fudev1.Channel) (domain.Channel, bool) {
	s, ok := fromProto(channelPref, p)
	c := domain.Channel(s)
	return c, ok && c.Valid()
}

// StateToProto returns the enum for s.
func StateToProto(s domain.DraftState) fudev1.DraftState {
	return toProto[fudev1.DraftState](statePrefix, fudev1.DraftState_value, string(s))
}

// StateFromProto returns the domain state for p.
func StateFromProto(p fudev1.DraftState) (domain.DraftState, bool) {
	s, ok := fromProto(statePrefix, p)
	st := domain.DraftState(s)
	return st, ok && st.Valid()
}

// AuthorToProto returns the enum for a draft_versions.created_by value.
func AuthorToProto(createdBy string) fudev1.VersionAuthor {
	return toProto[fudev1.VersionAuthor](authorPrefix, fudev1.VersionAuthor_value, createdBy)
}
