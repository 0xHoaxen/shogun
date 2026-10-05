package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/services/soroban/internal/domain"
)

func TestPeriodBounds(t *testing.T) {
	loc := domain.Location()
	tests := []struct {
		name      string
		now       time.Time
		period    domain.Period
		wantStart time.Time
		wantEnd   time.Time
	}{
		{
			name:      "daily uses IST midnight, which is 18:30 UTC the day before",
			now:       time.Date(2026, 10, 5, 20, 0, 0, 0, time.UTC), // 01:30 IST on 6 Oct
			period:    domain.PeriodDaily,
			wantStart: time.Date(2026, 10, 5, 18, 30, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 10, 6, 18, 30, 0, 0, time.UTC),
		},
		{
			name:      "daily just before IST midnight stays on the same day",
			now:       time.Date(2026, 10, 5, 18, 29, 59, 0, time.UTC),
			period:    domain.PeriodDaily,
			wantStart: time.Date(2026, 10, 4, 18, 30, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 10, 5, 18, 30, 0, 0, time.UTC),
		},
		{
			name:      "monthly starts on the first in IST",
			now:       time.Date(2026, 10, 15, 12, 0, 0, 0, time.UTC),
			period:    domain.PeriodMonthly,
			wantStart: time.Date(2026, 9, 30, 18, 30, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 10, 31, 18, 30, 0, 0, time.UTC),
		},
		{
			name:      "monthly rolls over the year end",
			now:       time.Date(2026, 12, 20, 0, 0, 0, 0, time.UTC),
			period:    domain.PeriodMonthly,
			wantStart: time.Date(2026, 11, 30, 18, 30, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 12, 31, 18, 30, 0, 0, time.UTC),
		},
		{
			name:      "late on the last UTC day of a month is already next month in IST",
			now:       time.Date(2026, 10, 31, 19, 0, 0, 0, time.UTC),
			period:    domain.PeriodMonthly,
			wantStart: time.Date(2026, 10, 31, 18, 30, 0, 0, time.UTC),
			wantEnd:   time.Date(2026, 11, 30, 18, 30, 0, 0, time.UTC),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			start, end, err := domain.PeriodBounds(tt.now, tt.period, loc)
			if err != nil {
				t.Fatalf("PeriodBounds: %v", err)
			}
			if !start.Equal(tt.wantStart) || !end.Equal(tt.wantEnd) {
				t.Errorf("got [%s, %s), want [%s, %s)", start, end, tt.wantStart, tt.wantEnd)
			}
		})
	}
}

func TestPeriodBoundsRejectsUnknownPeriod(t *testing.T) {
	if _, _, err := domain.PeriodBounds(time.Now(), domain.Period("weekly"), domain.Location()); err == nil {
		t.Fatal("want an error for an unknown period")
	}
}

func TestCost(t *testing.T) {
	opus := domain.Price{InputMicrosPerMtok: 4_000_000, OutputMicrosPerMtok: 20_000_000, CacheReadMicrosPerMtok: 200_000, CacheWriteMicrosPerMtok: 5_000_000}
	tests := []struct {
		name  string
		usage domain.Usage
		want  int64
	}{
		{"zero usage costs nothing", domain.Usage{}, 0},
		{"one million input tokens cost the input rate", domain.Usage{InputTokens: 1_000_000}, 4_000_000},
		{"input and output add up", domain.Usage{InputTokens: 1000, OutputTokens: 500}, 4_000 + 10_000},
		{"a fraction of a micro-dollar rounds up", domain.Usage{CacheReadTokens: 1}, 1},
		{"all four kinds are priced", domain.Usage{InputTokens: 10, OutputTokens: 10, CacheReadTokens: 10, CacheWriteTokens: 10}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.Cost(opus, tt.usage)
			if err != nil {
				t.Fatalf("Cost: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %d, want %d", got, tt.want)
			}
		})
	}
}

func TestCostRejectsNegativeTokens(t *testing.T) {
	_, err := domain.Cost(domain.Price{}, domain.Usage{OutputTokens: -1})
	if !errors.Is(err, domain.ErrNegativeUsage) {
		t.Fatalf("got %v, want ErrNegativeUsage", err)
	}
}

func TestReservationCanSettleOnlyWhenOpen(t *testing.T) {
	tests := []struct {
		status domain.ReservationStatus
		want   bool
	}{
		{domain.ReservationOpen, true},
		{domain.ReservationCommitted, false},
		{domain.ReservationReleased, false},
		{domain.ReservationExpired, false},
	}
	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.CanSettle(); got != tt.want {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
