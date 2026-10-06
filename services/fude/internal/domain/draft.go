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

// Regenerate returns a pending draft with its row refreshed, for a new AI
// version that is still being written. Only a pending draft can be
// regenerated; a generating one already has a version on the way.
func (d Draft) Regenerate(now time.Time) (Draft, error) {
	if d.State != DraftPending {
		return d, d.invalidMove(DraftPending)
	}
	return d.Move(DraftPending, now)
}

// MarkPosted returns the draft sent, once the owner says they posted an
// approved copy-only draft themselves. An email draft is refused with a
// *TransitionError: only tsubame's report can mark an email sent.
func (d Draft) MarkPosted(channel Channel, now time.Time) (Draft, error) {
	if channel == ChannelEmail {
		return d, &TransitionError{
			Reason: ReasonDraftChannelNotCopyOnly,
			From:   string(d.State),
			To:     string(DraftSent),
		}
	}
	return d.Move(DraftSent, now)
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

// RecordSent returns the draft sent, once tsubame reports that version went
// out. changed is false when nothing needs to be written: the draft is already
// sent, or the report is for a version the draft is no longer on.
//
// A draft normally goes approved to sent. A pending draft on the same version
// is accepted too: it was put back to pending after a send looked lost, and the
// mail went out after all. The mail is out, so the draft says so. Any other
// state is a *TransitionError.
func (d Draft) RecordSent(version int32, now time.Time) (next Draft, changed bool, err error) {
	if d.State == DraftSent {
		return d, false, nil
	}
	if version != d.CurrentVersion {
		return d, false, nil
	}
	switch d.State {
	case DraftApproved, DraftPending:
		d.State = DraftSent
		d.UpdatedAt = now
		return d, true, nil
	default:
		return d, false, d.invalidMove(DraftSent)
	}
}

// RecordSendFailed returns an approved draft back to pending, once tsubame
// reports its send did not go out, so the owner can approve it again. changed
// is false when the report is stale: the draft is no longer approved on that
// version (it was edited or has moved on), and there is nothing to undo.
func (d Draft) RecordSendFailed(version int32, now time.Time) (next Draft, changed bool, err error) {
	if d.State != DraftApproved || version != d.CurrentVersion {
		return d, false, nil
	}
	moved, err := d.Move(DraftPending, now)
	return moved, err == nil, err
}
