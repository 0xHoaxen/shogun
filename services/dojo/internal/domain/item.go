package domain

import (
	"net/url"
	"strings"
	"time"
)

// Limits on item fields.
const (
	maxTitleLen   = 200
	maxURLLen     = 2000
	maxInsightLen = 5000
)

// ItemKind is what is being learned. Values match items.kind CHECK.
type ItemKind string

// Item kinds.
const (
	KindCourse  ItemKind = "course"
	KindBook    ItemKind = "book"
	KindProject ItemKind = "project"
	KindSkill   ItemKind = "skill"
)

// Valid reports whether k is a known kind.
func (k ItemKind) Valid() bool {
	switch k {
	case KindCourse, KindBook, KindProject, KindSkill:
		return true
	default:
		return false
	}
}

// ItemStatus is how far an item has come. Values match items.status CHECK.
type ItemStatus string

// Item statuses.
const (
	StatusPlanned    ItemStatus = "planned"
	StatusInProgress ItemStatus = "in_progress"
	StatusDone       ItemStatus = "done"
)

// itemTransitions follows docs/shogun-state-machines.md.
var itemTransitions = transitions[ItemStatus]{
	StatusPlanned:    {StatusInProgress, StatusDone},
	StatusInProgress: {StatusPlanned, StatusDone},
	StatusDone:       {StatusInProgress}, // reopened
}

// Valid reports whether s is a known status.
func (s ItemStatus) Valid() bool {
	_, ok := itemTransitions[s]
	return ok
}

// CanMoveTo reports whether an item in status s may move to next.
func (s ItemStatus) CanMoveTo(next ItemStatus) bool {
	return itemTransitions.allows(s, next)
}

// ItemInput is what the owner types for an item.
type ItemInput struct {
	Title   string
	Kind    ItemKind
	URL     string
	Insight string
}

// Validate returns the input trimmed, or an error wrapping ErrInvalid.
func (in ItemInput) Validate() (ItemInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.URL = strings.TrimSpace(in.URL)
	in.Insight = strings.TrimSpace(in.Insight)
	switch {
	case in.Title == "":
		return in, invalid("title is required")
	case len(in.Title) > maxTitleLen:
		return in, invalid("title is longer than %d bytes", maxTitleLen)
	case !in.Kind.Valid():
		return in, invalid("kind %q is not known", in.Kind)
	case len(in.Insight) > maxInsightLen:
		return in, invalid("insight is longer than %d bytes", maxInsightLen)
	case in.URL != "" && !webURL(in.URL):
		return in, invalid("url must be an http or https address of at most %d bytes", maxURLLen)
	}
	return in, nil
}

func webURL(raw string) bool {
	if len(raw) > maxURLLen {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// Item is something being learned. Methods return a changed copy.
type Item struct {
	Status      ItemStatus
	StartedOn   *time.Time
	CompletedOn *time.Time
}

// ChangeStatus returns the item moved to next on the date today, or a
// *TransitionError. Starting stamps StartedOn once, finishing stamps
// CompletedOn, and moving off a stage clears the date that stage set.
func (i Item) ChangeStatus(next ItemStatus, today time.Time) (Item, error) {
	if !i.Status.CanMoveTo(next) {
		return i, &TransitionError{
			Reason: ReasonItemStatusInvalidTransition,
			From:   string(i.Status),
			To:     string(next),
		}
	}
	i.Status = next
	switch next {
	case StatusPlanned:
		i.StartedOn, i.CompletedOn = nil, nil
	case StatusInProgress:
		i.CompletedOn = nil
		if i.StartedOn == nil {
			i.StartedOn = &today
		}
	case StatusDone:
		i.CompletedOn = &today
	}
	return i, nil
}
