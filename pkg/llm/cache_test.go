package llm_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/pkg/llm"
)

func TestMemoryCacheExpiresEntries(t *testing.T) {
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	cache := llm.NewMemoryCache(func() time.Time { return now })
	cache.Set("k", llm.Response{Text: "x"}, time.Hour)

	tests := []struct {
		name  string
		after time.Duration
		want  bool
	}{
		{"before expiry", 59 * time.Minute, true},
		{"at expiry", time.Hour, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			now = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC).Add(tt.after)
			if _, ok := cache.Get("k"); ok != tt.want {
				t.Errorf("hit = %v, want %v", ok, tt.want)
			}
		})
	}
}

func TestMemoryCacheStopsGrowingWhenFullOfLiveEntries(t *testing.T) {
	cache := llm.NewMemoryCache(nil)
	for i := range 600 {
		cache.Set(fmt.Sprintf("k%d", i), llm.Response{Text: "x"}, time.Hour)
	}

	hits := 0
	for i := range 600 {
		if _, ok := cache.Get(fmt.Sprintf("k%d", i)); ok {
			hits++
		}
	}

	if hits != 512 {
		t.Errorf("%d entries kept, want the 512 cap", hits)
	}
}

func TestMemoryCacheReplacesAnExistingKeyEvenWhenFull(t *testing.T) {
	cache := llm.NewMemoryCache(nil)
	for i := range 512 {
		cache.Set(fmt.Sprintf("k%d", i), llm.Response{Text: "old"}, time.Hour)
	}

	cache.Set("k0", llm.Response{Text: "new"}, time.Hour)

	if got, _ := cache.Get("k0"); got.Text != "new" {
		t.Errorf("k0 = %q, want new", got.Text)
	}
}
