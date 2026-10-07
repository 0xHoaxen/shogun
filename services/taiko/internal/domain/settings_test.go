package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/services/taiko/internal/domain"
)

func TestChannelSettingsValidate(t *testing.T) {
	tests := []struct {
		name    string
		in      domain.ChannelSettings
		wantErr bool
	}{
		{"defaults", domain.DefaultChannelSettings(), false},
		{"no quiet hours", domain.ChannelSettings{InAppEnabled: false}, false},
		{"daytime window", domain.ChannelSettings{Quiet: &domain.QuietHours{From: 9 * time.Hour, To: 17 * time.Hour}}, false},
		{"window wrapping midnight", domain.ChannelSettings{Quiet: &domain.QuietHours{From: 22 * time.Hour, To: 8 * time.Hour}}, false},
		{"last minute of the day", domain.ChannelSettings{Quiet: &domain.QuietHours{From: 0, To: 23*time.Hour + 59*time.Minute}}, false},
		{"equal ends", domain.ChannelSettings{Quiet: &domain.QuietHours{From: 8 * time.Hour, To: 8 * time.Hour}}, true},
		{"negative start", domain.ChannelSettings{Quiet: &domain.QuietHours{From: -time.Minute, To: time.Hour}}, true},
		{"end at a full day", domain.ChannelSettings{Quiet: &domain.QuietHours{From: time.Hour, To: 24 * time.Hour}}, true},
		{"seconds", domain.ChannelSettings{Quiet: &domain.QuietHours{From: time.Hour, To: 2*time.Hour + time.Second}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.in.Validate()
			if tt.wantErr && !errors.Is(err, domain.ErrInvalidQuietHours) {
				t.Fatalf("want ErrInvalidQuietHours, got %v", err)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("want valid, got %v", err)
			}
		})
	}
}
