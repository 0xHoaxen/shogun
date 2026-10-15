package connectapi_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/app/apptest"
	"github.com/0xHoaxen/shogun/services/torii/internal/domain"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/connectapi"
)

type adminHarness struct {
	client      apiv1connect.AdminServiceClient
	users       *apptest.MemUsers
	auth        *app.Auth
	adminToken  string
	adminID     string
	memberToken string
	memberID    string
}

func newAdminHarness(t *testing.T) *adminHarness {
	t.Helper()
	now := time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)
	users := apptest.NewMemUsers()
	auth, err := app.NewAuth(apptest.NewMemSessions(), users,
		app.AuthConfig{AdminEmails: []string{ownerEmail}, SessionTTL: sessionTTL},
		app.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	adminToken, admin, err := auth.StartSession(context.Background(), app.Claims{Subject: "sub-admin", Email: ownerEmail, EmailVerified: true})
	if err != nil {
		t.Fatalf("admin StartSession: %v", err)
	}
	memberToken, member, err := auth.StartSession(context.Background(), app.Claims{Subject: "sub-member", Email: "member@example.com", EmailVerified: true})
	if err != nil {
		t.Fatalf("member StartSession: %v", err)
	}
	log := slog.New(slog.DiscardHandler)
	mux := http.NewServeMux()
	mux.Handle(apiv1connect.NewAdminServiceHandler(connectapi.NewAdminServer(users, log),
		connect.WithInterceptors(connectapi.NewSessionInterceptor(connectapi.InterceptorConfig{Auth: auth, Log: log}))))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &adminHarness{
		client: apiv1connect.NewAdminServiceClient(srv.Client(), srv.URL), users: users, auth: auth,
		adminToken: adminToken, adminID: admin.OwnerID.String(),
		memberToken: memberToken, memberID: member.OwnerID.String(),
	}
}

func TestAdminRPCsRefuseMembers(t *testing.T) {
	h := newAdminHarness(t)
	ctx := context.Background()
	calls := map[string]func() error{
		"ListUsers": func() error {
			_, err := h.client.ListUsers(ctx, withCookie(connect.NewRequest(&apiv1.ListUsersRequest{}), h.memberToken))
			return err
		},
		"SetUserStatus": func() error {
			_, err := h.client.SetUserStatus(ctx, withCookie(connect.NewRequest(&apiv1.SetUserStatusRequest{Id: h.adminID, Status: apiv1.UserStatus_USER_STATUS_DISABLED}), h.memberToken))
			return err
		},
		"SetGmailAllowed": func() error {
			_, err := h.client.SetGmailAllowed(ctx, withCookie(connect.NewRequest(&apiv1.SetGmailAllowedRequest{Id: h.memberID, GmailAllowed: true}), h.memberToken))
			return err
		},
	}
	for name, call := range calls {
		t.Run(name, func(t *testing.T) {
			err := call()

			if connect.CodeOf(err) != connect.CodePermissionDenied || reasonOf(err) != "ADMIN_REQUIRED" {
				t.Fatalf("code %v reason %q (err %v), want PermissionDenied ADMIN_REQUIRED", connect.CodeOf(err), reasonOf(err), err)
			}
		})
	}
	if user, _ := h.users.ByID(ctx, mustUUID(t, h.memberID)); user.GmailAllowed {
		t.Fatal("a member approved their own Gmail access")
	}
}

func TestAdminRPCsRequireASession(t *testing.T) {
	h := newAdminHarness(t)

	_, err := h.client.ListUsers(context.Background(), connect.NewRequest(&apiv1.ListUsersRequest{}))

	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("code %v, want Unauthenticated", connect.CodeOf(err))
	}
}

func TestListUsersPagesAndMapsFields(t *testing.T) {
	// Arrange
	h := newAdminHarness(t)
	ctx := context.Background()

	// Act
	first, err := h.client.ListUsers(ctx, withCookie(connect.NewRequest(&apiv1.ListUsersRequest{PageSize: 1}), h.adminToken))
	if err != nil {
		t.Fatalf("ListUsers page 1: %v", err)
	}
	second, err := h.client.ListUsers(ctx, withCookie(connect.NewRequest(&apiv1.ListUsersRequest{PageSize: 1, PageToken: first.Msg.GetNextPageToken()}), h.adminToken))
	if err != nil {
		t.Fatalf("ListUsers page 2: %v", err)
	}

	// Assert
	if len(first.Msg.GetUsers()) != 1 || first.Msg.GetNextPageToken() == "" || len(second.Msg.GetUsers()) != 1 || second.Msg.GetNextPageToken() != "" {
		t.Fatalf("pages %d/%d, tokens %q/%q", len(first.Msg.GetUsers()), len(second.Msg.GetUsers()), first.Msg.GetNextPageToken(), second.Msg.GetNextPageToken())
	}
	admin := first.Msg.GetUsers()[0]
	if admin.GetId() != h.adminID || admin.GetRole() != apiv1.UserRole_USER_ROLE_ADMIN || admin.GetStatus() != apiv1.UserStatus_USER_STATUS_ACTIVE || !admin.GetGmailAllowed() {
		t.Fatalf("admin = %v", admin)
	}
	if member := second.Msg.GetUsers()[0]; member.GetRole() != apiv1.UserRole_USER_ROLE_MEMBER || member.GetGmailAllowed() || member.GetEmail() != "member@example.com" {
		t.Fatalf("member = %v", member)
	}
}

func TestListUsersRejectsABadPageToken(t *testing.T) {
	h := newAdminHarness(t)

	_, err := h.client.ListUsers(context.Background(), withCookie(connect.NewRequest(&apiv1.ListUsersRequest{PageToken: "garbage"}), h.adminToken))

	if connect.CodeOf(err) != connect.CodeInvalidArgument || reasonOf(err) != "PAGE_TOKEN_INVALID" {
		t.Fatalf("code %v reason %q, want InvalidArgument PAGE_TOKEN_INVALID", connect.CodeOf(err), reasonOf(err))
	}
}

func TestSetUserStatusDisablesAndSignsTheUserOut(t *testing.T) {
	// Arrange
	h := newAdminHarness(t)
	ctx := context.Background()

	// Act
	resp, err := h.client.SetUserStatus(ctx, withCookie(connect.NewRequest(&apiv1.SetUserStatusRequest{Id: h.memberID, Status: apiv1.UserStatus_USER_STATUS_DISABLED}), h.adminToken))

	// Assert
	if err != nil || resp.Msg.GetUser().GetStatus() != apiv1.UserStatus_USER_STATUS_DISABLED {
		t.Fatalf("got %v, %v", resp.Msg, err)
	}
	if _, _, authErr := h.auth.Authenticate(ctx, h.memberToken); authErr == nil {
		t.Fatal("the disabled user's session still works")
	}
}

func TestAdminCannotDisableThemselves(t *testing.T) {
	h := newAdminHarness(t)

	_, err := h.client.SetUserStatus(context.Background(), withCookie(connect.NewRequest(&apiv1.SetUserStatusRequest{Id: h.adminID, Status: apiv1.UserStatus_USER_STATUS_DISABLED}), h.adminToken))

	if connect.CodeOf(err) != connect.CodeFailedPrecondition || reasonOf(err) != "ADMIN_CANNOT_DISABLE_SELF" {
		t.Fatalf("code %v reason %q, want FailedPrecondition ADMIN_CANNOT_DISABLE_SELF", connect.CodeOf(err), reasonOf(err))
	}
}

func TestSetUserStatusValidatesTheRequest(t *testing.T) {
	h := newAdminHarness(t)
	tests := []struct {
		name       string
		req        *apiv1.SetUserStatusRequest
		wantCode   connect.Code
		wantReason string
	}{
		{"malformed id", &apiv1.SetUserStatusRequest{Id: "nope", Status: apiv1.UserStatus_USER_STATUS_DISABLED}, connect.CodeInvalidArgument, "INVALID_USER_ID"},
		{"unspecified status", &apiv1.SetUserStatusRequest{Id: h.memberID}, connect.CodeInvalidArgument, "INVALID_USER_STATUS"},
		{"unknown user", &apiv1.SetUserStatusRequest{Id: "0197f0a0-0000-7000-8000-000000000001", Status: apiv1.UserStatus_USER_STATUS_DISABLED}, connect.CodeNotFound, "USER_NOT_FOUND"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.SetUserStatus(context.Background(), withCookie(connect.NewRequest(tt.req), h.adminToken))

			if connect.CodeOf(err) != tt.wantCode || reasonOf(err) != tt.wantReason {
				t.Fatalf("code %v reason %q, want %v %s", connect.CodeOf(err), reasonOf(err), tt.wantCode, tt.wantReason)
			}
		})
	}
}

func TestSetGmailAllowedApprovesAndWithdraws(t *testing.T) {
	// Arrange
	h := newAdminHarness(t)
	ctx := context.Background()

	// Act
	approved, err := h.client.SetGmailAllowed(ctx, withCookie(connect.NewRequest(&apiv1.SetGmailAllowedRequest{Id: h.memberID, GmailAllowed: true}), h.adminToken))
	if err != nil {
		t.Fatalf("approve: %v", err)
	}
	withdrawn, err := h.client.SetGmailAllowed(ctx, withCookie(connect.NewRequest(&apiv1.SetGmailAllowedRequest{Id: h.memberID, GmailAllowed: false}), h.adminToken))

	// Assert
	if err != nil || !approved.Msg.GetUser().GetGmailAllowed() || withdrawn.Msg.GetUser().GetGmailAllowed() {
		t.Fatalf("approved %v, withdrawn %v, err %v", approved.Msg.GetUser().GetGmailAllowed(), withdrawn.Msg.GetUser().GetGmailAllowed(), err)
	}
	if user, _ := h.users.ByID(ctx, mustUUID(t, h.memberID)); user.Status != domain.StatusActive {
		t.Fatalf("status = %q, want it untouched", user.Status)
	}
}

func mustUUID(t *testing.T, value string) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(value)
	if err != nil {
		t.Fatalf("parse uuid %q: %v", value, err)
	}
	return id
}
