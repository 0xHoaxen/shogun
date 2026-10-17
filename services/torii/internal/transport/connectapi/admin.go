package connectapi

import (
	"context"
	"errors"
	"log/slog"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	"github.com/0xHoaxen/shogun/services/torii/internal/domain"
	"github.com/0xHoaxen/shogun/services/torii/internal/store"
)

const (
	defaultUserPageSize = 50
	maxUserPageSize     = 200
)

// Reasons for the admin and Gmail-approval errors.
const (
	reasonAdminRequired       = "ADMIN_REQUIRED"
	reasonAdminDisableSelf    = "ADMIN_CANNOT_DISABLE_SELF"
	reasonInvalidUserID       = "INVALID_USER_ID"
	reasonInvalidUserStatus   = "INVALID_USER_STATUS"
	reasonUserNotFound        = "USER_NOT_FOUND"
	reasonPageTokenInvalid    = "PAGE_TOKEN_INVALID"
	reasonGmailNotApproved    = "GMAIL_NOT_APPROVED"
	messageAdminRequired      = "this needs an admin"
	messageGmailNotApproved   = "an admin has not approved Gmail access for your account yet"
	messageAdminDisableSelf   = "you cannot disable your own account"
	messageInvalidUserID      = "id is not a valid user id"
	messageInvalidUserStatus  = "status must be active or disabled"
	messageUserNotFound       = "no such user"
	messagePageTokenMalformed = "page_token is not valid"
)

// UserDirectory is the part of the users store AdminServer uses.
type UserDirectory interface {
	List(ctx context.Context, pageSize int32, pageToken string) ([]domain.User, string, error)
	SetStatus(ctx context.Context, id uuid.UUID, status domain.Status) (domain.User, error)
	SetGmailAllowed(ctx context.Context, id uuid.UUID, allowed bool) (domain.User, error)
}

// AdminServer implements shogun.api.v1.AdminService. Every call needs an admin
// session and exposes accounts only, never another user's content.
type AdminServer struct {
	users UserDirectory
	log   *slog.Logger
}

var _ apiv1connect.AdminServiceHandler = (*AdminServer)(nil)

// NewAdminServer returns an AdminServer that manages users through users.
func NewAdminServer(users UserDirectory, log *slog.Logger) *AdminServer {
	return &AdminServer{users: users, log: log}
}

// ListUsers pages through all users, oldest first.
func (s *AdminServer) ListUsers(
	ctx context.Context, req *connect.Request[apiv1.ListUsersRequest],
) (*connect.Response[apiv1.ListUsersResponse], error) {
	if _, err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	pageSize := req.Msg.GetPageSize()
	if pageSize <= 0 {
		pageSize = defaultUserPageSize
	}
	pageSize = min(pageSize, maxUserPageSize)
	users, next, err := s.users.List(ctx, pageSize, req.Msg.GetPageToken())
	if errors.Is(err, store.ErrBadPageToken) {
		return nil, newError(connect.CodeInvalidArgument, reasonPageTokenInvalid, messagePageTokenMalformed)
	}
	if err != nil {
		return nil, s.internal(ctx, "list users", err)
	}
	out := make([]*apiv1.User, len(users))
	for i, user := range users {
		out[i] = toAPIUser(user)
	}
	return connect.NewResponse(&apiv1.ListUsersResponse{Users: out, NextPageToken: next}), nil
}

// SetUserStatus enables or disables a user. The user's sessions stop working
// on their next request.
func (s *AdminServer) SetUserStatus(
	ctx context.Context, req *connect.Request[apiv1.SetUserStatusRequest],
) (*connect.Response[apiv1.SetUserStatusResponse], error) {
	admin, err := requireAdmin(ctx)
	if err != nil {
		return nil, err
	}
	id, err := uuid.Parse(req.Msg.GetId())
	if err != nil {
		return nil, newError(connect.CodeInvalidArgument, reasonInvalidUserID, messageInvalidUserID)
	}
	status, ok := domainStatus(req.Msg.GetStatus())
	if !ok {
		return nil, newError(connect.CodeInvalidArgument, reasonInvalidUserStatus, messageInvalidUserStatus)
	}
	if id == admin.OwnerID && status == domain.StatusDisabled {
		return nil, newError(connect.CodeFailedPrecondition, reasonAdminDisableSelf, messageAdminDisableSelf)
	}
	user, err := s.users.SetStatus(ctx, id, status)
	if errors.Is(err, store.ErrNotFound) {
		return nil, newError(connect.CodeNotFound, reasonUserNotFound, messageUserNotFound)
	}
	if err != nil {
		return nil, s.internal(ctx, "set user status", err)
	}
	return connect.NewResponse(&apiv1.SetUserStatusResponse{User: toAPIUser(user)}), nil
}

// SetGmailAllowed approves or withdraws a user's right to connect Gmail.
func (s *AdminServer) SetGmailAllowed(
	ctx context.Context, req *connect.Request[apiv1.SetGmailAllowedRequest],
) (*connect.Response[apiv1.SetGmailAllowedResponse], error) {
	if _, err := requireAdmin(ctx); err != nil {
		return nil, err
	}
	id, err := uuid.Parse(req.Msg.GetId())
	if err != nil {
		return nil, newError(connect.CodeInvalidArgument, reasonInvalidUserID, messageInvalidUserID)
	}
	user, err := s.users.SetGmailAllowed(ctx, id, req.Msg.GetGmailAllowed())
	if errors.Is(err, store.ErrNotFound) {
		return nil, newError(connect.CodeNotFound, reasonUserNotFound, messageUserNotFound)
	}
	if err != nil {
		return nil, s.internal(ctx, "set gmail allowed", err)
	}
	return connect.NewResponse(&apiv1.SetGmailAllowedResponse{User: toAPIUser(user)}), nil
}

func (s *AdminServer) internal(ctx context.Context, what string, err error) error {
	s.log.ErrorContext(ctx, what, slog.Any("error", err))
	return newError(connect.CodeInternal, reasonInternal, "something went wrong")
}

// requireAdmin returns the caller's session if they are an admin, or the
// error to send back.
func requireAdmin(ctx context.Context) (domain.Session, error) {
	session, ok := SessionFromContext(ctx)
	if !ok {
		return domain.Session{}, newError(connect.CodeUnauthenticated, reasonUnauthenticated, "sign in required")
	}
	if session.Role != domain.RoleAdmin {
		return domain.Session{}, newError(connect.CodePermissionDenied, reasonAdminRequired, messageAdminRequired)
	}
	return session, nil
}

func domainStatus(status apiv1.UserStatus) (domain.Status, bool) {
	switch status { //nolint:exhaustive // UNSPECIFIED and unknown values are invalid
	case apiv1.UserStatus_USER_STATUS_ACTIVE:
		return domain.StatusActive, true
	case apiv1.UserStatus_USER_STATUS_DISABLED:
		return domain.StatusDisabled, true
	default:
		return "", false
	}
}

func toAPIUser(user domain.User) *apiv1.User {
	role := apiv1.UserRole_USER_ROLE_MEMBER
	if user.Role == domain.RoleAdmin {
		role = apiv1.UserRole_USER_ROLE_ADMIN
	}
	status := apiv1.UserStatus_USER_STATUS_ACTIVE
	if user.Status == domain.StatusDisabled {
		status = apiv1.UserStatus_USER_STATUS_DISABLED
	}
	return &apiv1.User{
		Id:           user.ID.String(),
		Email:        user.Email,
		DisplayName:  user.DisplayName,
		Role:         role,
		Status:       status,
		GmailAllowed: user.GmailAllowed,
		Timezone:     user.Timezone,
		Version:      user.Version,
		CreatedAt:    timestamppb.New(user.CreatedAt),
	}
}
