package jobs

import "time"

// scanDate is the calendar date at now in loc, as the scans' job args carry it.
func scanDate(now time.Time, loc *time.Location) string {
	return now.In(loc).Format(time.DateOnly)
}
