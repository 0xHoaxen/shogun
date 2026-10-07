package connectapi_test

import (
	"context"
	"sync"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	taikov1 "github.com/0xHoaxen/shogun/gen/go/shogun/taiko/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
)

// settingsFake is the settings half of fakeTaiko.
type settingsFake struct {
	mu       sync.Mutex
	resp     *taikov1.ChannelSettings
	err      error
	saveReq  *taikov1.SaveChannelSettingsRequest
	identity authz.Identity
}

func (f *fakeTaiko) GetChannelSettings(ctx context.Context, _ *taikov1.GetChannelSettingsRequest, _ ...grpc.CallOption) (*taikov1.GetChannelSettingsResponse, error) {
	f.settings.mu.Lock()
	defer f.settings.mu.Unlock()
	f.settings.identity, _ = authz.FromContext(ctx)
	if f.settings.err != nil {
		return nil, f.settings.err
	}
	return &taikov1.GetChannelSettingsResponse{Settings: f.settings.resp}, nil
}

func (f *fakeTaiko) SaveChannelSettings(ctx context.Context, in *taikov1.SaveChannelSettingsRequest, _ ...grpc.CallOption) (*taikov1.SaveChannelSettingsResponse, error) {
	f.settings.mu.Lock()
	defer f.settings.mu.Unlock()
	f.settings.saveReq = in
	f.settings.identity, _ = authz.FromContext(ctx)
	if f.settings.err != nil {
		return nil, f.settings.err
	}
	return &taikov1.SaveChannelSettingsResponse{Settings: f.settings.resp}, nil
}

func TestGetNotificationSettingsMapsTheSettingsForTheSignedInOwner(t *testing.T) {
	h := newNotificationsHarness(t)
	h.taiko.settings.resp = &taikov1.ChannelSettings{
		InAppEnabled: true, Version: 4, Quiet: &taikov1.QuietHours{FromMinute: 1320, ToMinute: 480},
	}

	resp, err := h.client.GetNotificationSettings(context.Background(),
		withCookie(connect.NewRequest(&apiv1.GetNotificationSettingsRequest{}), h.token))
	if err != nil {
		t.Fatalf("GetNotificationSettings: %v", err)
	}
	got := resp.Msg.GetSettings()
	if !got.GetInAppEnabled() || got.GetVersion() != 4 || got.GetQuiet().GetFromMinute() != 1320 || got.GetQuiet().GetToMinute() != 480 {
		t.Fatalf("got %+v", got)
	}
	if h.taiko.settings.identity.OwnerID != h.owner {
		t.Fatalf("taiko saw owner %q, want %q", h.taiko.settings.identity.OwnerID, h.owner)
	}
}

func TestGetNotificationSettingsWithoutQuietHoursLeavesThemUnset(t *testing.T) {
	h := newNotificationsHarness(t)
	h.taiko.settings.resp = &taikov1.ChannelSettings{InAppEnabled: true}

	resp, err := h.client.GetNotificationSettings(context.Background(),
		withCookie(connect.NewRequest(&apiv1.GetNotificationSettingsRequest{}), h.token))

	if err != nil || resp.Msg.GetSettings().GetQuiet() != nil {
		t.Fatalf("got %+v, %v; want no quiet hours", resp.Msg.GetSettings(), err)
	}
}

func TestSaveNotificationSettingsPassesEveryFieldToTaiko(t *testing.T) {
	h := newNotificationsHarness(t)
	h.taiko.settings.resp = &taikov1.ChannelSettings{InAppEnabled: false, Version: 3}

	resp, err := h.client.SaveNotificationSettings(context.Background(), withCookie(connect.NewRequest(&apiv1.SaveNotificationSettingsRequest{
		Settings: &apiv1.NotificationSettings{
			InAppEnabled: false, Version: 2, Quiet: &apiv1.QuietHours{FromMinute: 60, ToMinute: 120},
		},
	}), h.token))
	if err != nil {
		t.Fatalf("SaveNotificationSettings: %v", err)
	}
	sent := h.taiko.settings.saveReq.GetSettings()
	if sent.GetInAppEnabled() || sent.GetVersion() != 2 || sent.GetQuiet().GetFromMinute() != 60 || sent.GetQuiet().GetToMinute() != 120 {
		t.Fatalf("taiko was sent %+v", sent)
	}
	if resp.Msg.GetSettings().GetVersion() != 3 || h.taiko.settings.identity.OwnerID != h.owner {
		t.Fatalf("got %+v for owner %q", resp.Msg.GetSettings(), h.taiko.settings.identity.OwnerID)
	}
}

func TestSaveNotificationSettingsKeepsTheReasonOfARefusal(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode connect.Code
		reason   string
	}{
		{"stale version", withInfo(codes.Aborted, "VERSION_CONFLICT"), connect.CodeAborted, "VERSION_CONFLICT"},
		{"invalid window", withInfo(codes.InvalidArgument, "CHANNEL_SETTINGS_INVALID"), connect.CodeInvalidArgument, "CHANNEL_SETTINGS_INVALID"},
		{"internal failure hides its text", withInfo(codes.Internal, "INTERNAL"), connect.CodeInternal, "INTERNAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newNotificationsHarness(t)
			h.taiko.settings.err = tt.err

			_, err := h.client.SaveNotificationSettings(context.Background(), withCookie(connect.NewRequest(
				&apiv1.SaveNotificationSettingsRequest{Settings: &apiv1.NotificationSettings{}}), h.token))

			if code, reason := codeAndReason(t, err); code != tt.wantCode || reason != tt.reason {
				t.Fatalf("got %v %q, want %v %q", code, reason, tt.wantCode, tt.reason)
			}
		})
	}
}

func TestNotificationSettingsNeedASession(t *testing.T) {
	h := newNotificationsHarness(t)

	_, getErr := h.client.GetNotificationSettings(context.Background(), connect.NewRequest(&apiv1.GetNotificationSettingsRequest{}))
	_, saveErr := h.client.SaveNotificationSettings(context.Background(), connect.NewRequest(&apiv1.SaveNotificationSettingsRequest{}))

	if connect.CodeOf(getErr) != connect.CodeUnauthenticated || connect.CodeOf(saveErr) != connect.CodeUnauthenticated {
		t.Fatalf("got %v and %v, want Unauthenticated for both", getErr, saveErr)
	}
	if h.taiko.settings.saveReq != nil {
		t.Fatal("taiko was called without a session")
	}
}
