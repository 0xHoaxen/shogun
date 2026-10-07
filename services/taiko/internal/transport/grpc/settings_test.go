package grpc_test

import (
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	taikov1 "github.com/0xHoaxen/shogun/gen/go/shogun/taiko/v1"
)

func TestGetChannelSettingsDefaultsToInAppOnAtVersionZero(t *testing.T) {
	h := newHarness(t)

	res, err := h.client.GetChannelSettings(h.ctx(t), &taikov1.GetChannelSettingsRequest{})

	got := res.GetSettings()
	if err != nil || !got.GetInAppEnabled() || got.GetQuiet() != nil || got.GetVersion() != 0 {
		t.Fatalf("got %v, %v; want in-app on, no quiet hours, version 0", got, err)
	}
}

func TestSaveChannelSettingsRoundTripsPerOwner(t *testing.T) {
	h := newHarness(t)
	quiet := &taikov1.QuietHours{FromMinute: 22 * 60, ToMinute: 8*60 + 30}

	saved, err := h.client.SaveChannelSettings(h.ctx(t), &taikov1.SaveChannelSettingsRequest{
		Settings: &taikov1.ChannelSettings{InAppEnabled: false, Quiet: quiet},
	})
	read, readErr := h.client.GetChannelSettings(h.ctx(t), &taikov1.GetChannelSettingsRequest{})
	other, otherErr := h.client.GetChannelSettings(h.ctxFor(t, uuid.NewString()), &taikov1.GetChannelSettingsRequest{})

	if err != nil || readErr != nil || otherErr != nil {
		t.Fatalf("errors %v, %v, %v", err, readErr, otherErr)
	}
	got := read.GetSettings()
	if got.GetInAppEnabled() || got.GetVersion() != 1 || got.GetVersion() != saved.GetSettings().GetVersion() {
		t.Fatalf("read back %v, saved %v", got, saved.GetSettings())
	}
	if q := got.GetQuiet(); q.GetFromMinute() != quiet.GetFromMinute() || q.GetToMinute() != quiet.GetToMinute() {
		t.Fatalf("quiet hours read back as %v, want %v", q, quiet)
	}
	if !other.GetSettings().GetInAppEnabled() || other.GetSettings().GetVersion() != 0 {
		t.Fatalf("another owner sees %v; settings are per owner", other.GetSettings())
	}
}

func TestSaveChannelSettingsRefusals(t *testing.T) {
	h := newHarness(t)
	if _, err := h.client.SaveChannelSettings(h.ctx(t), &taikov1.SaveChannelSettingsRequest{
		Settings: &taikov1.ChannelSettings{InAppEnabled: true},
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tests := []struct {
		name     string
		settings *taikov1.ChannelSettings
		code     codes.Code
		reason   string
	}{
		{"stale version", &taikov1.ChannelSettings{Version: 7}, codes.Aborted, "VERSION_CONFLICT"},
		{"create over an existing row", &taikov1.ChannelSettings{Version: 0}, codes.Aborted, "VERSION_CONFLICT"},
		{"equal ends", &taikov1.ChannelSettings{Version: 1, Quiet: &taikov1.QuietHours{FromMinute: 60, ToMinute: 60}}, codes.InvalidArgument, "CHANNEL_SETTINGS_INVALID"},
		{"minute past the day", &taikov1.ChannelSettings{Version: 1, Quiet: &taikov1.QuietHours{FromMinute: 0, ToMinute: 24 * 60}}, codes.InvalidArgument, "CHANNEL_SETTINGS_INVALID"},
		{"negative version", &taikov1.ChannelSettings{Version: -1}, codes.InvalidArgument, "CHANNEL_SETTINGS_INVALID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.SaveChannelSettings(h.ctx(t), &taikov1.SaveChannelSettingsRequest{Settings: tt.settings})

			requireStatus(t, err, tt.code, tt.reason)
		})
	}
}

func TestChannelSettingsNeedAnIdentity(t *testing.T) {
	h := newHarness(t)
	_, getErr := h.client.GetChannelSettings(t.Context(), &taikov1.GetChannelSettingsRequest{})
	_, saveErr := h.client.SaveChannelSettings(t.Context(), &taikov1.SaveChannelSettingsRequest{})

	requireStatus(t, getErr, codes.Unauthenticated, "")
	requireStatus(t, saveErr, codes.Unauthenticated, "")
}
