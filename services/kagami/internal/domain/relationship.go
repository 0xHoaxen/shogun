package domain

import (
	"strings"
	"unicode"
)

// The relationship values a contact may have. They match the
// contacts.relationship CHECK.
const (
	RelationshipRecruiter = "recruiter"
	RelationshipEngineer  = "engineer"
	RelationshipManager   = "manager"
	RelationshipOther     = "other"
)

// relationshipRules are checked in order, and the first group with a matching
// word wins, so "Engineering Manager" is a manager and "Head of Talent" a
// recruiter. A prefix ends in "*" and matches any word that starts with it;
// other entries must equal a whole word.
var relationshipRules = []struct {
	relationship string
	words        []string
}{
	{RelationshipRecruiter, []string{"recruit*", "talent", "sourc*"}},
	{RelationshipManager, []string{"manag*", "head", "director", "vp", "lead", "cto"}},
	{RelationshipEngineer, []string{"engineer*", "developer", "dev", "swe"}},
}

// GuessRelationship guesses how the owner knows someone from their job title.
// It looks at whole words only, so "ahead" is not "head". guessed is false when
// there was nothing to go on (an empty title); a title that matches no rule is
// "other", and counts as guessed.
func GuessRelationship(role string) (relationship string, guessed bool) {
	words := strings.FieldsFunc(strings.ToLower(role), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	if len(words) == 0 {
		return RelationshipOther, false
	}
	for _, rule := range relationshipRules {
		for _, want := range rule.words {
			for _, word := range words {
				if matchesWord(word, want) {
					return rule.relationship, true
				}
			}
		}
	}
	return RelationshipOther, true
}

func matchesWord(word, want string) bool {
	if prefix, ok := strings.CutSuffix(want, "*"); ok {
		return strings.HasPrefix(word, prefix)
	}
	return word == want
}

// NormalizeName lower-cases a person's name and collapses runs of space, so
// "  Priya   RAMAN " and "priya raman" are the same name.
func NormalizeName(name string) string {
	return strings.Join(strings.Fields(strings.ToLower(name)), " ")
}
