package domain

import (
	"fmt"
	"math"
	"strings"
	"unicode"
)

// How much each part of a posting's match is worth. They add up to one.
const (
	weightRole     = 0.35
	weightLocation = 0.20
	weightMustHave = 0.30
	weightNice     = 0.15

	// BorderlineWidth is how far below MinScore a rule score may fall and still
	// be worth a second opinion from the model.
	BorderlineWidth float32 = 0.2
)

// Scorers, as scores.scored_by names them.
const (
	ScoredByRule = "rule"
	ScoredByLLM  = "llm"
)

// Score is how well a posting fits the preferences, with the reasons.
type Score struct {
	Value   float32
	Reasons []string
}

// Outcome says what a score means for the owner.
type Outcome int

// Outcomes of classifying a score.
const (
	// Reject is too far below the minimum to be worth more attention.
	Reject Outcome = iota
	// Borderline is just short of the minimum; a model may judge it.
	Borderline
	// Match is at or above the minimum.
	Match
)

// Classify says whether a score is a match, borderline or rejected under the
// preferences' minimum.
func (p Preferences) Classify(score float32) Outcome {
	switch {
	case score >= p.MinScore:
		return Match
	case score >= p.MinScore-BorderlineWidth:
		return Borderline
	default:
		return Reject
	}
}

// Rule scores a candidate against the preferences by plain matching. Nothing
// matches when no preference is set. An excluded term scores zero.
func Rule(p Preferences, c Candidate) Score {
	if p.Empty() {
		return Score{Reasons: []string{"no preferences are set"}}
	}
	title := strings.ToLower(c.Title)
	body := strings.ToLower(c.Title + " " + c.Company + " " + c.Description)
	if hit := firstFound(p.Exclude, body); hit != "" {
		return Score{Reasons: []string{"mentions the excluded term " + hit}}
	}

	var total float64
	var reasons []string
	add := func(weight float64, earned float64, reason string) {
		total += weight * earned
		reasons = append(reasons, reason)
	}

	if len(p.Roles) > 0 {
		if hit := firstFound(p.Roles, title); hit != "" {
			add(weightRole, 1, "the title matches the wanted role "+hit)
		} else {
			add(weightRole, 0, "the title names none of the wanted roles")
		}
	} else {
		total += weightRole
	}

	if len(p.Locations) > 0 {
		where := strings.ToLower(c.Location + " " + c.Title)
		if hit := firstFound(p.Locations, where); hit != "" {
			add(weightLocation, 1, "the location matches "+hit)
		} else {
			add(weightLocation, 0, "the location is none of the wanted ones")
		}
	} else {
		total += weightLocation
	}

	total += mustHave(p.MustHave, body, &reasons)
	total += niceToHave(p.NiceToHave, body, &reasons)

	return Score{Value: round2(total), Reasons: reasons}
}

func mustHave(terms []string, body string, reasons *[]string) float64 {
	if len(terms) == 0 {
		return weightMustHave
	}
	var missing []string
	for _, t := range terms {
		if !found(body, t) {
			missing = append(missing, t)
		}
	}
	if len(missing) == 0 {
		*reasons = append(*reasons, "it mentions every required term")
	} else {
		*reasons = append(*reasons, "it lacks "+strings.Join(missing, ", "))
	}
	return weightMustHave * float64(len(terms)-len(missing)) / float64(len(terms))
}

func niceToHave(terms []string, body string, reasons *[]string) float64 {
	if len(terms) == 0 {
		return weightNice
	}
	var have int
	for _, t := range terms {
		if found(body, t) {
			have++
		}
	}
	*reasons = append(*reasons, fmt.Sprintf("it mentions %d of %d nice-to-have terms", have, len(terms)))
	return weightNice * float64(have) / float64(len(terms))
}

func round2(v float64) float32 {
	return float32(math.Round(v*100) / 100)
}

// firstFound returns the first term found in text, or "".
func firstFound(terms []string, text string) string {
	for _, t := range terms {
		if found(text, t) {
			return t
		}
	}
	return ""
}

// found reports whether term occurs in text as a whole word or phrase, so "go"
// is not found in "google". Both are already lower case.
func found(text, term string) bool {
	if term == "" {
		return false
	}
	for from := 0; ; {
		i := strings.Index(text[from:], term)
		if i < 0 {
			return false
		}
		start, end := from+i, from+i+len(term)
		if boundaryBefore(text, start) && boundaryAfter(text, end) {
			return true
		}
		from = start + 1
	}
}

func boundaryBefore(text string, i int) bool {
	if i == 0 {
		return true
	}
	r := lastRune(text[:i])
	return !isWordRune(r)
}

func boundaryAfter(text string, i int) bool {
	if i >= len(text) {
		return true
	}
	r := firstRune(text[i:])
	return !isWordRune(r)
}

func isWordRune(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }

func firstRune(s string) rune {
	for _, r := range s {
		return r
	}
	return 0
}

func lastRune(s string) rune {
	var last rune
	for _, r := range s {
		last = r
	}
	return last
}
