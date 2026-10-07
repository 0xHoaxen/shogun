// Package schedule holds time schedules shared by the services' periodic jobs.
package schedule

import (
	"time"
	// The service image has no tz database; embed the one schedules need.
	_ "time/tzdata"
)

// Daily fires once a day at a wall-clock time in a location. It satisfies
// river.PeriodicSchedule.
type Daily struct {
	Hour, Minute int
	Loc          *time.Location
}

// Next returns the first firing strictly after t.
func (d Daily) Next(t time.Time) time.Time {
	local := t.In(d.Loc)
	next := time.Date(local.Year(), local.Month(), local.Day(), d.Hour, d.Minute, 0, 0, d.Loc)
	if !next.After(local) {
		next = time.Date(local.Year(), local.Month(), local.Day()+1, d.Hour, d.Minute, 0, 0, d.Loc)
	}
	return next
}
