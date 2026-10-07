package domain

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ErrInvalid marks input that breaks a domain rule.
var ErrInvalid = errors.New("invalid input")

// ReasonSuggestionAlreadyDecided is the stable ErrorInfo.reason for deciding a
// suggestion that is no longer open.
const ReasonSuggestionAlreadyDecided = "SUGGESTION_ALREADY_DECIDED"

// Limits on suggestion fields.
const (
	maxTextLen     = 4000
	maxReasonLen   = 1000
	maxEvidence    = 10
	maxLabelLen    = 200
	maxEvidenceURL = 2000
)

// Target is the document a suggestion is about. Values match suggestions.target.
type Target string

// Targets.
const (
	TargetResume   Target = "resume"
	TargetLinkedIn Target = "linkedin"
)

// Valid reports whether t is a known target.
func (t Target) Valid() bool { return t == TargetResume || t == TargetLinkedIn }

// Section is the part of the document a suggestion changes.
type Section string

// Sections.
const (
	SectionHeadline   Section = "headline"
	SectionAbout      Section = "about"
	SectionExperience Section = "experience"
	SectionSkills     Section = "skills"
	SectionProjects   Section = "projects"
)

// Valid reports whether s is a known section.
func (s Section) Valid() bool {
	switch s {
	case SectionHeadline, SectionAbout, SectionExperience, SectionSkills, SectionProjects:
		return true
	default:
		return false
	}
}

// State is where a suggestion stands. Values match suggestions.state.
type State string

// States.
const (
	StateOpen      State = "open"
	StateAccepted  State = "accepted"
	StateDismissed State = "dismissed"
)

// Valid reports whether s is a known state.
func (s State) Valid() bool { return s == StateOpen || s == StateAccepted || s == StateDismissed }

// DecisionError reports a decision on a suggestion that is not open. Transport
// maps it to FailedPrecondition carrying Reason.
type DecisionError struct {
	Reason string
	From   State
	To     State
}

func (e *DecisionError) Error() string {
	return fmt.Sprintf("%s: %s -> %s", e.Reason, e.From, e.To)
}

// Decide returns the state after the owner decides on a suggestion in s. Only
// an open suggestion can be accepted or dismissed, and only once.
func (s State) Decide(to State) (State, error) {
	if s != StateOpen || (to != StateAccepted && to != StateDismissed) {
		return s, &DecisionError{Reason: ReasonSuggestionAlreadyDecided, From: s, To: to}
	}
	return to, nil
}

// Evidence links a suggestion to what it is based on.
type Evidence struct {
	Label string `json:"label"`
	URL   string `json:"url"`
}

// Suggestion is a proposed change to the owner's resume or LinkedIn profile.
type Suggestion struct {
	Target   Target
	Section  Section
	Before   string
	After    string
	Reason   string
	Evidence []Evidence
}

// Validate returns the suggestion trimmed, or an error wrapping ErrInvalid. A
// suggestion must say what to write and why, and its evidence must be web links.
func (s Suggestion) Validate() (Suggestion, error) {
	s.Before, s.After, s.Reason = strings.TrimSpace(s.Before), strings.TrimSpace(s.After), strings.TrimSpace(s.Reason)
	switch {
	case !s.Target.Valid():
		return s, invalid("target %q is not known", s.Target)
	case !s.Section.Valid():
		return s, invalid("section %q is not known", s.Section)
	case s.After == "":
		return s, invalid("after is required")
	case s.Reason == "":
		return s, invalid("reason is required")
	case len(s.After) > maxTextLen || len(s.Before) > maxTextLen:
		return s, invalid("text is longer than %d bytes", maxTextLen)
	case len(s.Reason) > maxReasonLen:
		return s, invalid("reason is longer than %d bytes", maxReasonLen)
	case len(s.Evidence) > maxEvidence:
		return s, invalid("more than %d evidence links", maxEvidence)
	}
	kept := make([]Evidence, 0, len(s.Evidence))
	for _, e := range s.Evidence {
		e.Label, e.URL = strings.TrimSpace(e.Label), strings.TrimSpace(e.URL)
		if e.Label == "" || len(e.Label) > maxLabelLen || !webURL(e.URL) {
			return s, invalid("evidence needs a label and an http or https link")
		}
		kept = append(kept, e)
	}
	s.Evidence = kept
	return s, nil
}

func webURL(raw string) bool {
	if len(raw) > maxEvidenceURL {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}
