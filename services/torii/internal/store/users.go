package store

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/torii/internal/domain"
	"github.com/0xHoaxen/shogun/services/torii/internal/store/db"
)

// ErrBadPageToken is returned when a page token was not issued by List.
var ErrBadPageToken = errors.New("store: bad page token")

// Users reads and writes torii's users table.
type Users struct {
	q *db.Queries
}

// NewUsers returns a Users store that runs on d.
func NewUsers(d DBTX) *Users { return &Users{q: db.New(d)} }

// Upsert creates the user, or refreshes the email and display name of the
// existing one. Role, status, Gmail approval and timezone are kept.
func (u *Users) Upsert(ctx context.Context, user domain.User) (domain.User, error) {
	row, err := u.q.UpsertUser(ctx, db.UpsertUserParams{
		ID:          user.ID,
		GoogleSub:   user.GoogleSub,
		Email:       user.Email,
		DisplayName: user.DisplayName,
	})
	if err != nil {
		return domain.User{}, fmt.Errorf("upsert user: %w", err)
	}
	return toUser(row), nil
}

// ByID returns the user, or ErrNotFound.
func (u *Users) ByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	row, err := u.q.GetUser(ctx, id)
	if err != nil {
		return domain.User{}, notFoundOr(err, "get user")
	}
	return toUser(row), nil
}

// List returns up to pageSize users oldest first, and the token for the next
// page, which is empty on the last page.
func (u *Users) List(ctx context.Context, pageSize int32, pageToken string) ([]domain.User, string, error) {
	afterCreated, afterID, err := decodePageToken(pageToken)
	if err != nil {
		return nil, "", err
	}
	rows, err := u.q.ListUsers(ctx, db.ListUsersParams{
		AfterCreatedAt: afterCreated,
		AfterID:        afterID,
		PageSize:       pageSize + 1,
	})
	if err != nil {
		return nil, "", fmt.Errorf("list users: %w", err)
	}
	next := ""
	if int32(len(rows)) > pageSize {
		rows = rows[:pageSize]
		last := rows[len(rows)-1]
		next = encodePageToken(last.CreatedAt, last.ID)
	}
	users := make([]domain.User, len(rows))
	for i, row := range rows {
		users[i] = toUser(row)
	}
	return users, next, nil
}

// SetRole changes the user's role and returns the updated user.
func (u *Users) SetRole(ctx context.Context, id uuid.UUID, role domain.Role) (domain.User, error) {
	row, err := u.q.SetUserRole(ctx, db.SetUserRoleParams{ID: id, Role: string(role)})
	if err != nil {
		return domain.User{}, notFoundOr(err, "set user role")
	}
	return toUser(row), nil
}

// SetStatus enables or disables the user and returns the updated user.
func (u *Users) SetStatus(ctx context.Context, id uuid.UUID, status domain.Status) (domain.User, error) {
	row, err := u.q.SetUserStatus(ctx, db.SetUserStatusParams{ID: id, Status: string(status)})
	if err != nil {
		return domain.User{}, notFoundOr(err, "set user status")
	}
	return toUser(row), nil
}

// SetGmailAllowed records the admin's Gmail approval and returns the updated user.
func (u *Users) SetGmailAllowed(ctx context.Context, id uuid.UUID, allowed bool) (domain.User, error) {
	row, err := u.q.SetUserGmailAllowed(ctx, db.SetUserGmailAllowedParams{ID: id, GmailAllowed: allowed})
	if err != nil {
		return domain.User{}, notFoundOr(err, "set gmail allowed")
	}
	return toUser(row), nil
}

// SetTimezone changes the zone the user's days are counted in.
func (u *Users) SetTimezone(ctx context.Context, id uuid.UUID, timezone string) (domain.User, error) {
	row, err := u.q.SetUserTimezone(ctx, db.SetUserTimezoneParams{ID: id, Timezone: timezone})
	if err != nil {
		return domain.User{}, notFoundOr(err, "set user timezone")
	}
	return toUser(row), nil
}

func notFoundOr(err error, what string) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return fmt.Errorf("%s: %w", what, err)
}

func toUser(row db.User) domain.User {
	return domain.User{
		ID:           row.ID,
		GoogleSub:    row.GoogleSub,
		Email:        row.Email,
		DisplayName:  row.DisplayName,
		Role:         domain.Role(row.Role),
		Status:       domain.Status(row.Status),
		GmailAllowed: row.GmailAllowed,
		Timezone:     row.Timezone,
		Version:      row.Version,
		CreatedAt:    row.CreatedAt,
		UpdatedAt:    row.UpdatedAt,
	}
}

func encodePageToken(createdAt time.Time, id uuid.UUID) string {
	raw := createdAt.UTC().Format(time.RFC3339Nano) + "|" + id.String()
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// decodePageToken returns the keyset position after which to read. An empty
// token starts from the beginning.
func decodePageToken(token string) (time.Time, uuid.UUID, error) {
	if token == "" {
		return time.Time{}, uuid.Nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrBadPageToken
	}
	createdPart, idPart, ok := strings.Cut(string(raw), "|")
	if !ok {
		return time.Time{}, uuid.Nil, ErrBadPageToken
	}
	createdAt, err := time.Parse(time.RFC3339Nano, createdPart)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrBadPageToken
	}
	id, err := uuid.Parse(idPart)
	if err != nil {
		return time.Time{}, uuid.Nil, ErrBadPageToken
	}
	return createdAt, id, nil
}
