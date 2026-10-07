package domain

import (
	"sort"
	"strings"
	"time"
)

// Rolled-up metric names.
const (
	MetricJobsAdded    = "jobs_added"
	MetricApplications = "applications"
	MetricShortlisted  = "shortlisted"
	MetricInterviews   = "interviews"
	MetricOffers       = "offers"
	MetricRejections   = "rejections"
	MetricOutreachSent = "outreach_sent"
	MetricReplies      = "replies"
	MetricContactMoves = "contact_moves"
)

// FunnelMetrics are the metrics a funnel reads.
var FunnelMetrics = []string{
	MetricJobsAdded, MetricApplications, MetricShortlisted, MetricInterviews, MetricOffers, MetricRejections,
}

// OutreachMetrics are the metrics outreach stats read.
var OutreachMetrics = []string{MetricOutreachSent, MetricReplies, MetricContactMoves}

// Range limits for stat queries.
const (
	// DefaultRangeDays is how far back a query goes when it names no start.
	DefaultRangeDays = 30
	// MaxRangeDays is the longest range a query may cover.
	MaxRangeDays = 366
)

// Rollup is one row of daily_rollups.
type Rollup struct {
	Day       time.Time
	Metric    string
	Dimension string
	Value     int64
}

// value reads the part after key= of a dimension such as "source=linkedin".
func (r Rollup) value(key string) string {
	v, ok := strings.CutPrefix(r.Dimension, key+"=")
	if !ok || v == "" {
		return Unknown
	}
	return v
}

// FunnelRow counts jobs at each stage for one group.
type FunnelRow struct {
	Key                                                                  string
	JobsAdded, Applications, Shortlisted, Interviews, Offers, Rejections int64
}

// InterviewRate is the share of applications that led to an interview.
func (r FunnelRow) InterviewRate() float64 { return ratio(r.Interviews, r.Applications) }

// OfferRate is the share of applications that led to an offer.
func (r FunnelRow) OfferRate() float64 { return ratio(r.Offers, r.Applications) }

func (r *FunnelRow) add(metric string, v int64) {
	switch metric {
	case MetricJobsAdded:
		r.JobsAdded += v
	case MetricApplications:
		r.Applications += v
	case MetricShortlisted:
		r.Shortlisted += v
	case MetricInterviews:
		r.Interviews += v
	case MetricOffers:
		r.Offers += v
	case MetricRejections:
		r.Rejections += v
	}
}

// FunnelGroup is what a funnel is grouped by.
type FunnelGroup int

// Funnel groupings.
const (
	FunnelBySource FunnelGroup = iota + 1
	FunnelByMonth
)

// Funnel groups rollups into rows, sorted by key, and totals them.
func Funnel(rows []Rollup, by FunnelGroup) ([]FunnelRow, FunnelRow) {
	groups := map[string]*FunnelRow{}
	total := FunnelRow{Key: "total"}
	for _, r := range rows {
		key := r.value("source")
		if by == FunnelByMonth {
			key = r.Day.Format("2006-01")
		}
		g := groups[key]
		if g == nil {
			g = &FunnelRow{Key: key}
			groups[key] = g
		}
		g.add(r.Metric, r.Value)
		total.add(r.Metric, r.Value)
	}
	out := make([]FunnelRow, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, total
}

// OutreachRow counts outreach for one channel or status.
type OutreachRow struct {
	Key           string
	Sent, Replied int64
	// MovedIn counts moves into the status, when grouped by status.
	MovedIn int64
}

// ReplyRate is replies over first contacts made.
func (r OutreachRow) ReplyRate() float64 { return ratio(r.Replied, r.Sent) }

// OutreachGroup is what outreach stats are grouped by.
type OutreachGroup int

// Outreach groupings.
const (
	OutreachByChannel OutreachGroup = iota + 1
	OutreachByStatus
)

// Outreach groups rollups into rows, sorted by key, and totals them. Grouped by
// channel the rows count sent and replied; grouped by status they count moves
// into each status. The total always counts sent and replied.
func Outreach(rows []Rollup, by OutreachGroup) ([]OutreachRow, OutreachRow) {
	groups := map[string]*OutreachRow{}
	total := OutreachRow{Key: "total"}
	for _, r := range rows {
		switch r.Metric {
		case MetricOutreachSent:
			total.Sent += r.Value
		case MetricReplies:
			total.Replied += r.Value
		}
		var key string
		switch {
		case by == OutreachByChannel && (r.Metric == MetricOutreachSent || r.Metric == MetricReplies):
			key = r.value("channel")
		case by == OutreachByStatus && r.Metric == MetricContactMoves:
			key = r.value("status")
		default:
			continue
		}
		g := groups[key]
		if g == nil {
			g = &OutreachRow{Key: key}
			groups[key] = g
		}
		switch r.Metric {
		case MetricOutreachSent:
			g.Sent += r.Value
		case MetricReplies:
			g.Replied += r.Value
		case MetricContactMoves:
			g.MovedIn += r.Value
		}
	}
	out := make([]OutreachRow, 0, len(groups))
	for _, g := range groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, total
}

func ratio(part, whole int64) float64 {
	if whole <= 0 {
		return 0
	}
	return float64(part) / float64(whole)
}

// DayRange validates a query's range. Empty bounds default to the last
// DefaultRangeDays ending on today; both ends are included.
func DayRange(from, to string, today time.Time) (time.Time, time.Time, error) {
	end := today
	if to != "" {
		d, err := time.Parse(time.DateOnly, to)
		if err != nil {
			return time.Time{}, time.Time{}, invalidRange("to must be YYYY-MM-DD")
		}
		end = d
	}
	start := end.AddDate(0, 0, -(DefaultRangeDays - 1))
	if from != "" {
		d, err := time.Parse(time.DateOnly, from)
		if err != nil {
			return time.Time{}, time.Time{}, invalidRange("from must be YYYY-MM-DD")
		}
		start = d
	}
	if start.After(end) {
		return time.Time{}, time.Time{}, invalidRange("from is after to")
	}
	if end.Sub(start) > time.Duration(MaxRangeDays-1)*24*time.Hour {
		return time.Time{}, time.Time{}, invalidRange("the range is longer than a year")
	}
	return start, end, nil
}
