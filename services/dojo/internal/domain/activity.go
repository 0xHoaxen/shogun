package domain

import (
	"slices"
	"strings"
	"time"
)

// Limits on activity fields.
const (
	maxSummaryLen = 2000
	maxMinutes    = 24 * 60
	maxTags       = 10
	maxTagLen     = 40
)

// ActivityInput is what the owner types when logging an activity.
type ActivityInput struct {
	Summary    string
	Minutes    int32 // 0 means not recorded
	OccurredOn time.Time
	Tags       []string
}

// Validate returns the input cleaned up, or an error wrapping ErrInvalid. An
// empty OccurredOn becomes today, and a date after today is refused. Tags are
// lowercased, trimmed and de-duplicated, keeping their order.
func (in ActivityInput) Validate(today time.Time) (ActivityInput, error) {
	in.Summary = strings.TrimSpace(in.Summary)
	if in.OccurredOn.IsZero() {
		in.OccurredOn = today
	}
	switch {
	case in.Summary == "":
		return in, invalid("summary is required")
	case len(in.Summary) > maxSummaryLen:
		return in, invalid("summary is longer than %d bytes", maxSummaryLen)
	case in.Minutes < 0 || in.Minutes > maxMinutes:
		return in, invalid("minutes must be between 0 and %d", maxMinutes)
	case in.OccurredOn.After(today):
		return in, invalid("occurred_on is in the future")
	}
	tags, err := cleanTags(in.Tags)
	if err != nil {
		return in, err
	}
	in.Tags = tags
	return in, nil
}

func cleanTags(raw []string) ([]string, error) {
	tags := make([]string, 0, len(raw))
	for _, t := range raw {
		t = strings.ToLower(strings.TrimSpace(t))
		switch {
		case t == "":
			continue
		case len(t) > maxTagLen:
			return nil, invalid("tag is longer than %d bytes", maxTagLen)
		case slices.Contains(tags, t):
			continue
		}
		tags = append(tags, t)
	}
	if len(tags) > maxTags {
		return nil, invalid("more than %d tags", maxTags)
	}
	return tags, nil
}
