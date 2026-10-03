package jobs

import (
	"time"
	// The service image has no tz database; embed the one scans need.
	_ "time/tzdata"
)

// dailySchedule fires once a day at a wall-clock time in a location. It
// satisfies river.PeriodicSchedule.
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

// scanDate is the calendar date at now in loc, as the scans' job args carry it.
func scanDate(now time.Time, loc *time.Location) string {
	return now.In(loc).Format(time.DateOnly)
}
