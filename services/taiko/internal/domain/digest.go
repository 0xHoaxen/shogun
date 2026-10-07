package domain

import (
	"fmt"
	"strings"
)

const (
	microsPerCent  = 10_000
	centsPerDollar = 100
)

// Digest is what the daily digest summarises.
type Digest struct {
	// FollowUps is how many jobs and contacts are due or overdue.
	FollowUps int
	// Drafts is how many drafts wait for the owner; DraftsCapped says there
	// are more than were counted.
	Drafts       int
	DraftsCapped bool
	// SpendMicros is today's Claude spend in micro-dollars.
	SpendMicros int64
}

// Empty reports whether there is nothing to tell.
func (d Digest) Empty() bool {
	return d.FollowUps == 0 && d.Drafts == 0 && d.SpendMicros == 0
}

// Notice builds the digest notification. Parts with nothing in them are left
// out, and the link goes to the part that needs the owner most.
func (d Digest) Notice() (Notice, error) {
	var parts []string
	if d.FollowUps > 0 {
		parts = append(parts, plural(d.FollowUps, "follow-up", "follow-ups")+" due")
	}
	if d.Drafts > 0 {
		count := plural(d.Drafts, "draft", "drafts")
		if d.DraftsCapped {
			count = fmt.Sprintf("%d+ drafts", d.Drafts)
		}
		parts = append(parts, count+" waiting")
	}
	if d.SpendMicros > 0 {
		parts = append(parts, dollars(d.SpendMicros)+" spent")
	}
	if len(parts) == 0 {
		return Notice{}, ErrEmptyDigest
	}
	return NewNotice(TypeDailyDigest, "Daily digest", strings.Join(parts, ", ")+".", d.link())
}

func (d Digest) link() string {
	switch {
	case d.Drafts > 0:
		return "/drafts"
	case d.FollowUps > 0:
		return jobsRoute
	default:
		return spendRoute
	}
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

// dollars formats micro-dollars to the cent, and says "under $0.01" for a
// spend too small to show.
func dollars(micros int64) string {
	cents := (micros + microsPerCent/2) / microsPerCent
	if cents == 0 {
		return "under $0.01"
	}
	return fmt.Sprintf("$%d.%02d", cents/centsPerDollar, cents%centsPerDollar)
}
