package schedule_test

import (
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/pkg/schedule"
)

func TestDailyFiresOncePerLocalDay(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}
	at8 := schedule.Daily{Hour: 8, Loc: loc}
	tests := []struct {
		name string
		from time.Time
		want time.Time
	}{
		{"before the time, same day", time.Date(2026, 10, 3, 7, 0, 0, 0, loc), time.Date(2026, 10, 3, 8, 0, 0, 0, loc)},
		{"exactly at the time, next day", time.Date(2026, 10, 3, 8, 0, 0, 0, loc), time.Date(2026, 10, 4, 8, 0, 0, 0, loc)},
		{"after the time, next day", time.Date(2026, 10, 3, 9, 30, 0, 0, loc), time.Date(2026, 10, 4, 8, 0, 0, 0, loc)},
		{"given in UTC", time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC), time.Date(2026, 10, 3, 8, 0, 0, 0, loc)},
		{"month end", time.Date(2026, 10, 31, 12, 0, 0, 0, loc), time.Date(2026, 11, 1, 8, 0, 0, 0, loc)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := at8.Next(tt.from); !got.Equal(tt.want) {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}
