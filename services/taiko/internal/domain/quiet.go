package domain

import "time"

const day = 24 * time.Hour

// QuietHours is a window of the day in which nothing is sent. From and To are
// offsets from midnight in the owner's zone. A window may wrap past midnight,
// such as 22:00 to 08:00. The window includes From and excludes To, and
// From equal to To means no window at all.
type QuietHours struct {
	From, To time.Duration
}

// Quiet reports whether the time of day at falls inside the window.
func (q QuietHours) Quiet(at time.Duration) bool {
	switch {
	case q.From == q.To:
		return false
	case q.From < q.To:
		return at >= q.From && at < q.To
	default:
		return at >= q.From || at < q.To
	}
}

// Until returns how long it is from the time of day at until the window ends,
// or zero when at is outside the window.
func (q QuietHours) Until(at time.Duration) time.Duration {
	if !q.Quiet(at) {
		return 0
	}
	return ((q.To-at)%day + day) % day
}

// TimeOfDay is the offset from midnight of t in its own location.
func TimeOfDay(t time.Time) time.Duration {
	return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute + time.Duration(t.Second())*time.Second
}
