package domain

import "slices"

// Kind is what a draft is for. Values match the drafts.kind CHECK.
type Kind string

// The draft kinds.
const (
	KindCoverLetter Kind = "cover_letter"
	KindOutreach    Kind = "outreach"
	KindFollowUp    Kind = "follow_up"
	KindPost        Kind = "post"
	KindOneOff      Kind = "one_off"
)

// Valid reports whether k is a known kind.
func (k Kind) Valid() bool {
	return slices.Contains([]Kind{KindCoverLetter, KindOutreach, KindFollowUp, KindPost, KindOneOff}, k)
}

// Channel is where a draft goes. Values match the drafts.channel CHECK.
type Channel string

// The channels. Only email is sent by tsubame; the others are copy-only.
const (
	ChannelEmail    Channel = "email"
	ChannelLinkedIn Channel = "linkedin"
	ChannelX        Channel = "x"
	ChannelOther    Channel = "other"
)

// Valid reports whether c is a known channel.
func (c Channel) Valid() bool {
	return slices.Contains([]Channel{ChannelEmail, ChannelLinkedIn, ChannelX, ChannelOther}, c)
}

// TargetType is what a draft is about. Values match the drafts.target_type CHECK.
type TargetType string

// The target types.
const (
	TargetJob              TargetType = "job"
	TargetContact          TargetType = "contact"
	TargetLearningActivity TargetType = "learning_activity"
	TargetNone             TargetType = "none"
)

// Valid reports whether t is a known target type.
func (t TargetType) Valid() bool {
	return slices.Contains([]TargetType{TargetJob, TargetContact, TargetLearningActivity, TargetNone}, t)
}

// NeedsTargetID reports whether a draft of this type must name its target.
func (t TargetType) NeedsTargetID() bool { return t != TargetNone }
