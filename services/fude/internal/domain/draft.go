package domain

import (
	"slices"
	"time"
)

// DraftState is a stage in a draft's life. Values match the drafts.state CHECK.
type DraftState string

// The draft states.
const (
	DraftGenerating DraftState = "generating"
	DraftPending    DraftState = "pending"
	DraftApproved   DraftState = "approved"
	DraftSent       DraftState = "sent"
	DraftDiscarded  DraftState = "discarded"
	DraftFailed     DraftState = "failed"
)

// draftTransitions follows docs/shogun-state-machines.md. A state with no
// entry, or an empty list, has no way out.
var draftTransitions = map[DraftState][]DraftState{
	DraftGenerating: {DraftPending, DraftFailed},
	DraftFailed:     {DraftGenerating},                             // retry
	DraftPending:    {DraftPending, DraftApproved, DraftDiscarded}, // pending again is a new version
	DraftApproved:   {DraftSent, DraftPending},                     // pending again: edited after approval, or send failed
	DraftSent:       nil,
	DraftDiscarded:  nil,
}

// Valid reports whether s is a known draft state.
func (s DraftState) Valid() bool {
	_, ok := draftTransitions[s]
	return ok
}

// CanMoveTo reports whether a draft in state s may move to next.
func (s DraftState) CanMoveTo(next DraftState) bool {
	return slices.Contains(draftTransitions[s], next)
}

// Draft is a message being prepared. Methods return a changed copy.
type Draft struct {
	ID             string
	State          DraftState
	CurrentVersion int32
	FailureReason  string
	Version        int32
	UpdatedAt      time.Time
}

// Move returns the draft in state next, or a *TransitionError.
func (d Draft) Move(next DraftState, now time.Time) (Draft, error) {
	if !d.State.CanMoveTo(next) {
		return d, d.invalidMove(next)
	}
	d.State = next
	d.UpdatedAt = now
	return d, nil
}

// Generated returns the draft pending with its newest version recorded after
// the AI finished.
func (d Draft) Generated(version int32, now time.Time) (Draft, error) {
	if d.State != DraftGenerating {
		return d, d.invalidMove(DraftPending)
	}
	next, err := d.Move(DraftPending, now)
	if err != nil {
		return d, err
	}
	next.CurrentVersion = version
	next.FailureReason = ""
	return next, nil
}

// GenerationFailed returns the draft failed with the reason after the last
// try failed.
func (d Draft) GenerationFailed(reason string, now time.Time) (Draft, error) {
	next, err := d.Move(DraftFailed, now)
	if err != nil {
		return d, err
	}
	next.FailureReason = reason
	return next, nil
}

// Retry returns a failed draft generating again.
func (d Draft) Retry(now time.Time) (Draft, error) {
	next, err := d.Move(DraftGenerating, now)
	if err != nil {
		return d, err
	}
	next.FailureReason = ""
	return next, nil
}

// NewVersion returns the draft pending with a new newest version. It covers
// regenerate, an edit of a pending draft, and an edit after approval (which
// needs a fresh approval before anything leaves).
func (d Draft) NewVersion(version int32, now time.Time) (Draft, error) {
	if d.State != DraftPending && d.State != DraftApproved {
		return d, d.invalidMove(DraftPending)
	}
	next, err := d.Move(DraftPending, now)
	if err != nil {
		return d, err
	}
	next.CurrentVersion = version
	return next, nil
}

// CanApprove reports whether version is the newest version of a pending
// draft, the only thing that may be approved.
func (d Draft) CanApprove(version int32) bool {
	return d.State == DraftPending && version == d.CurrentVersion && version > 0
}

func (d Draft) invalidMove(to DraftState) *TransitionError {
	return &TransitionError{
		Reason: ReasonDraftStateInvalidTransition,
		From:   string(d.State),
		To:     string(to),
	}
}
