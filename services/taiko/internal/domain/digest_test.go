package domain

import (
	"errors"
	"testing"
	"time"
)

func TestDigestNotice(t *testing.T) {
	tests := []struct {
		name     string
		digest   Digest
		wantBody string
		wantLink string
	}{
		{"everything", Digest{FollowUps: 2, Drafts: 3, SpendMicros: 6_400_000}, "2 follow-ups due, 3 drafts waiting, $6.40 spent.", "/drafts"},
		{"singular", Digest{FollowUps: 1, Drafts: 1}, "1 follow-up due, 1 draft waiting.", "/drafts"},
		{"follow-ups only", Digest{FollowUps: 4}, "4 follow-ups due.", "/jobs"},
		{"spend only", Digest{SpendMicros: 1_500_000}, "$1.50 spent.", "/settings/spend"},
		{"capped drafts", Digest{Drafts: 200, DraftsCapped: true}, "200+ drafts waiting.", "/drafts"},
		{"a spend too small to show", Digest{SpendMicros: 3_000}, "under $0.01 spent.", "/settings/spend"},
		{"rounds to the cent", Digest{SpendMicros: 6_404_999}, "$6.40 spent.", "/settings/spend"},
		{"rounds up", Digest{SpendMicros: 6_405_000}, "$6.41 spent.", "/settings/spend"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.digest.Notice()
			if err != nil {
				t.Fatalf("Notice: %v", err)
			}
			if got.Type != TypeDailyDigest || got.Title != "Daily digest" || got.Body != tt.wantBody || got.Link != tt.wantLink {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestAnEmptyDigestHasNoNotice(t *testing.T) {
	d := Digest{}

	_, err := d.Notice()

	if !d.Empty() || !errors.Is(err, ErrEmptyDigest) {
		t.Fatalf("empty %v, err %v", d.Empty(), err)
	}
}

func hm(h, m int) time.Duration { return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute }

func TestQuietHours(t *testing.T) {
	overnight := QuietHours{From: hm(22, 0), To: hm(8, 0)}
	daytime := QuietHours{From: hm(8, 0), To: hm(9, 0)}
	tests := []struct {
		name      string
		window    QuietHours
		at        time.Duration
		wantQuiet bool
		wantUntil time.Duration
	}{
		{"overnight, before midnight", overnight, hm(23, 0), true, hm(9, 0)},
		{"overnight, after midnight", overnight, hm(2, 0), true, hm(6, 0)},
		{"overnight, at the start", overnight, hm(22, 0), true, hm(10, 0)},
		{"overnight, at the end", overnight, hm(8, 0), false, 0},
		{"overnight, midday", overnight, hm(12, 0), false, 0},
		{"daytime, inside", daytime, hm(8, 30), true, 30 * time.Minute},
		{"daytime, at the end", daytime, hm(9, 0), false, 0},
		{"daytime, before", daytime, hm(7, 59), false, 0},
		{"no window", QuietHours{From: hm(8, 0), To: hm(8, 0)}, hm(8, 0), false, 0},
		{"zero value", QuietHours{}, hm(3, 0), false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.window.Quiet(tt.at); got != tt.wantQuiet {
				t.Errorf("Quiet = %v, want %v", got, tt.wantQuiet)
			}
			if got := tt.window.Until(tt.at); got != tt.wantUntil {
				t.Errorf("Until = %s, want %s", got, tt.wantUntil)
			}
		})
	}
}

func TestTimeOfDay(t *testing.T) {
	loc := time.FixedZone("IST", 5*3600+1800)

	got := TimeOfDay(time.Date(2026, 10, 7, 8, 30, 15, 0, loc))

	if want := hm(8, 30) + 15*time.Second; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
