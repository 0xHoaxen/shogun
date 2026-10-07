package domain

import (
	"slices"
	"strings"
)

// Limits on preference fields.
const (
	maxTerms   = 30
	maxTermLen = 60

	// DefaultMinScore is the score at which a posting counts as a match until
	// the owner says otherwise.
	DefaultMinScore float32 = 0.7
)

// Preferences say what the owner is looking for.
type Preferences struct {
	Roles      []string
	Locations  []string
	MustHave   []string
	NiceToHave []string
	Exclude    []string
	// MinScore is the score from 0 to 1 at which a posting counts as a match.
	MinScore float32
}

// DefaultPreferences are what an owner who has set none has.
func DefaultPreferences() Preferences { return Preferences{MinScore: DefaultMinScore} }

// Empty reports whether no term is set, so nothing can be matched.
func (p Preferences) Empty() bool {
	return len(p.Roles)+len(p.Locations)+len(p.MustHave)+len(p.NiceToHave)+len(p.Exclude) == 0
}

// Validate returns the preferences cleaned up, or an error wrapping ErrInvalid.
// Terms are lower-cased, trimmed and de-duplicated, keeping their order.
func (p Preferences) Validate() (Preferences, error) {
	if p.MinScore < 0 || p.MinScore > 1 {
		return p, invalid("min_score must be between 0 and 1")
	}
	lists := []*[]string{&p.Roles, &p.Locations, &p.MustHave, &p.NiceToHave, &p.Exclude}
	for _, list := range lists {
		cleaned, err := cleanTerms(*list)
		if err != nil {
			return p, err
		}
		*list = cleaned
	}
	return p, nil
}

func cleanTerms(raw []string) ([]string, error) {
	out := make([]string, 0, len(raw))
	for _, t := range raw {
		t = strings.ToLower(strings.TrimSpace(t))
		switch {
		case t == "":
			continue
		case len(t) > maxTermLen:
			return nil, invalid("a term is longer than %d bytes", maxTermLen)
		case slices.Contains(out, t):
			continue
		}
		out = append(out, t)
	}
	if len(out) > maxTerms {
		return nil, invalid("more than %d terms in one list", maxTerms)
	}
	return out, nil
}
