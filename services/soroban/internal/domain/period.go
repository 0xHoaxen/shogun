package domain

import (
	"fmt"
	"time"
)

// Period is how often a budget resets.
type Period string

// Budget periods.
const (
	PeriodDaily   Period = "daily"
	PeriodMonthly Period = "monthly"
)

// Valid reports whether p is a known period.
func (p Period) Valid() bool { return p == PeriodDaily || p == PeriodMonthly }

// Mode is whether a budget blocks calls or only notifies.
type Mode string

// Budget modes.
const (
	ModeHard Mode = "hard"
	ModeSoft Mode = "soft"
)

// Valid reports whether m is a known mode.
func (m Mode) Valid() bool { return m == ModeHard || m == ModeSoft }

// ScopeType is what a budget is counted against.
type ScopeType string

// Budget scopes.
const (
	ScopeGlobal  ScopeType = "global"
	ScopeService ScopeType = "service"
	ScopeFeature ScopeType = "feature"
)

// Valid reports whether s is a known scope type.
func (s ScopeType) Valid() bool {
	return s == ScopeGlobal || s == ScopeService || s == ScopeFeature
}

// istOffset is the offset of Asia/Kolkata. India has no daylight saving time,
// so a fixed zone is exact and needs no tz database in the service image.
const istOffset = 5*time.Hour + 30*time.Minute

// Location is the zone the owner's days and months are counted in.
func Location() *time.Location {
	return time.FixedZone("Asia/Kolkata", int(istOffset/time.Second))
}

// PeriodBounds returns the half-open interval [start, end) of the period of p
// that contains now, on local midnight boundaries in loc.
func PeriodBounds(now time.Time, p Period, loc *time.Location) (start, end time.Time, err error) {
	local := now.In(loc)
	y, m, d := local.Date()
	switch p {
	case PeriodDaily:
		start = time.Date(y, m, d, 0, 0, 0, 0, loc)
		end = time.Date(y, m, d+1, 0, 0, 0, 0, loc)
	case PeriodMonthly:
		start = time.Date(y, m, 1, 0, 0, 0, 0, loc)
		end = time.Date(y, m+1, 1, 0, 0, 0, 0, loc)
	default:
		return time.Time{}, time.Time{}, fmt.Errorf("domain: unknown period %q", p)
	}
	return start.UTC(), end.UTC(), nil
}
