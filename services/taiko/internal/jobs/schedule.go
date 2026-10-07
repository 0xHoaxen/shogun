package jobs

import "time"

// dailySchedule fires once a day at a wall-clock time in a location. It
// satisfies river.PeriodicSchedule. Kagami has the same type; services cannot
// import each other, so it is repeated here until it moves to pkg.
type dailySchedule struct {
	hour, minute int
	loc          *time.Location
}

// Next returns the first firing strictly after t.
func (d dailySchedule) Next(t time.Time) time.Time {
	local := t.In(d.loc)
	next := time.Date(local.Year(), local.Month(), local.Day(), d.hour, d.minute, 0, 0, d.loc)
	if !next.After(local) {
		next = time.Date(local.Year(), local.Month(), local.Day()+1, d.hour, d.minute, 0, 0, d.loc)
	}
	return next
}
