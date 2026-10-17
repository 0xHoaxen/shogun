package owners

import "time"

// Due is an owner whose own day has reached a time, with that day's date.
type Due struct {
	Owner Owner
	// Date is the owner's local calendar date, YYYY-MM-DD.
	Date string
}

// DueAt returns the owners whose clock at now is at or past hour:minute of
// their day, each with their local date. A scheduled job calls it on a short
// tick and enqueues one job per result, unique by owner and date, so the work
// runs once per owner per day whatever their time zone.
func DueAt(list []Owner, now time.Time, hour, minute int) []Due {
	var due []Due
	for _, o := range list {
		if o.ReachedLocalTime(now, hour, minute) {
			due = append(due, Due{Owner: o, Date: o.LocalDate(now)})
		}
	}
	return due
}
