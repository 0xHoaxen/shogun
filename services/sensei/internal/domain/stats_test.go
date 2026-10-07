package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/services/sensei/internal/domain"
)

func day(m time.Month, d int) time.Time { return time.Date(2026, m, d, 0, 0, 0, 0, time.UTC) }

func TestFunnelGroupsBySourceAndRatesAreOfApplications(t *testing.T) {
	rows := []domain.Rollup{
		{Day: day(10, 1), Metric: domain.MetricJobsAdded, Dimension: "source=linkedin", Value: 10},
		{Day: day(10, 1), Metric: domain.MetricApplications, Dimension: "source=linkedin", Value: 4},
		{Day: day(10, 2), Metric: domain.MetricApplications, Dimension: "source=linkedin", Value: 4},
		{Day: day(10, 3), Metric: domain.MetricInterviews, Dimension: "source=linkedin", Value: 2},
		{Day: day(10, 3), Metric: domain.MetricOffers, Dimension: "source=linkedin", Value: 1},
		{Day: day(10, 3), Metric: domain.MetricApplications, Dimension: "source=referral", Value: 2},
		{Day: day(10, 3), Metric: domain.MetricRejections, Dimension: "source=referral", Value: 1},
		{Day: day(10, 3), Metric: domain.MetricShortlisted, Dimension: "bogus", Value: 3},
	}

	got, total := domain.Funnel(rows, domain.FunnelBySource)

	if len(got) != 3 || got[0].Key != "linkedin" || got[1].Key != "referral" || got[2].Key != "unknown" {
		t.Fatalf("rows = %+v, want linkedin, referral, unknown sorted", got)
	}
	li := got[0]
	if li.JobsAdded != 10 || li.Applications != 8 || li.Interviews != 2 || li.Offers != 1 || li.InterviewRate() != 0.25 || li.OfferRate() != 0.125 {
		t.Fatalf("linkedin = %+v", li)
	}
	if got[2].Shortlisted != 3 || got[1].Rejections != 1 {
		t.Fatalf("others = %+v %+v", got[1], got[2])
	}
	if total.Key != "total" || total.Applications != 10 || total.JobsAdded != 10 || total.Interviews != 2 || total.InterviewRate() != 0.2 {
		t.Fatalf("total = %+v", total)
	}
}

func TestFunnelGroupsByMonthAndAnEmptyFunnelHasZeroRates(t *testing.T) {
	rows := []domain.Rollup{
		{Day: day(9, 30), Metric: domain.MetricApplications, Dimension: "source=x", Value: 1},
		{Day: day(10, 1), Metric: domain.MetricApplications, Dimension: "source=y", Value: 2},
	}

	got, _ := domain.Funnel(rows, domain.FunnelByMonth)
	none, total := domain.Funnel(nil, domain.FunnelBySource)

	if len(got) != 2 || got[0].Key != "2026-09" || got[1].Key != "2026-10" || got[1].Applications != 2 {
		t.Fatalf("rows = %+v", got)
	}
	if len(none) != 0 || total.InterviewRate() != 0 || total.OfferRate() != 0 {
		t.Fatalf("empty = %+v %+v", none, total)
	}
}

func TestOutreachByChannelCountsSentAndRepliesAndTheRate(t *testing.T) {
	rows := []domain.Rollup{
		{Day: day(10, 1), Metric: domain.MetricOutreachSent, Dimension: "channel=email", Value: 6},
		{Day: day(10, 2), Metric: domain.MetricOutreachSent, Dimension: "channel=email", Value: 2},
		{Day: day(10, 2), Metric: domain.MetricReplies, Dimension: "channel=email", Value: 4},
		{Day: day(10, 2), Metric: domain.MetricOutreachSent, Dimension: "channel=linkedin", Value: 4},
		{Day: day(10, 2), Metric: domain.MetricContactMoves, Dimension: "status=replied", Value: 9},
	}

	got, total := domain.Outreach(rows, domain.OutreachByChannel)

	if len(got) != 2 || got[0].Key != "email" || got[0].Sent != 8 || got[0].Replied != 4 || got[0].ReplyRate() != 0.5 ||
		got[1].Key != "linkedin" || got[1].ReplyRate() != 0 {
		t.Fatalf("rows = %+v", got)
	}
	if total.Sent != 12 || total.Replied != 4 || total.ReplyRate() != float64(4)/12 {
		t.Fatalf("total = %+v", total)
	}
}

func TestOutreachByStatusCountsMovesAndStillTotalsSentAndReplied(t *testing.T) {
	rows := []domain.Rollup{
		{Day: day(10, 1), Metric: domain.MetricContactMoves, Dimension: "status=reached_out", Value: 5},
		{Day: day(10, 2), Metric: domain.MetricContactMoves, Dimension: "status=reached_out", Value: 1},
		{Day: day(10, 2), Metric: domain.MetricContactMoves, Dimension: "status=replied", Value: 2},
		{Day: day(10, 2), Metric: domain.MetricOutreachSent, Dimension: "channel=email", Value: 6},
		{Day: day(10, 2), Metric: domain.MetricReplies, Dimension: "channel=email", Value: 2},
	}

	got, total := domain.Outreach(rows, domain.OutreachByStatus)

	if len(got) != 2 || got[0].Key != "reached_out" || got[0].MovedIn != 6 || got[1].Key != "replied" || got[1].MovedIn != 2 {
		t.Fatalf("rows = %+v", got)
	}
	if total.Sent != 6 || total.Replied != 2 {
		t.Fatalf("total = %+v", total)
	}
}

func TestDayRange(t *testing.T) {
	today := day(10, 7)
	tests := []struct {
		name     string
		from, to string
		wantFrom time.Time
		wantTo   time.Time
		wantErr  bool
	}{
		{"defaults to the last thirty days", "", "", day(9, 8), day(10, 7), false},
		{"explicit", "2026-10-01", "2026-10-05", day(10, 1), day(10, 5), false},
		{"only to", "", "2026-10-05", day(9, 6), day(10, 5), false},
		{"only from", "2026-10-01", "", day(10, 1), day(10, 7), false},
		{"a single day", "2026-10-05", "2026-10-05", day(10, 5), day(10, 5), false},
		{"from after to", "2026-10-06", "2026-10-05", time.Time{}, time.Time{}, true},
		{"not a date", "yesterday", "", time.Time{}, time.Time{}, true},
		{"to not a date", "", "2026-13-40", time.Time{}, time.Time{}, true},
		{"just a year", "2025-10-07", "2026-10-07", day(10, 7).AddDate(-1, 0, 0), day(10, 7), false},
		{"over a year", "2025-10-06", "2026-10-07", time.Time{}, time.Time{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to, err := domain.DayRange(tt.from, tt.to, today)

			if (err != nil) != tt.wantErr || (err != nil && !errors.Is(err, domain.ErrInvalidRange)) {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && (!from.Equal(tt.wantFrom) || !to.Equal(tt.wantTo)) {
				t.Fatalf("range %v..%v, want %v..%v", from, to, tt.wantFrom, tt.wantTo)
			}
		})
	}
}
