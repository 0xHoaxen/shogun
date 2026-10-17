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
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/protobuf/proto"

	toriiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/torii/v1"
	"github.com/0xHoaxen/shogun/pkg/outbox"
	"github.com/0xHoaxen/shogun/pkg/postgres"

	"github.com/0xHoaxen/shogun/services/torii/internal/domain"
	"github.com/0xHoaxen/shogun/services/torii/internal/store/db"
)

// ErrBadPageToken is returned when a page token was not issued by List.
var ErrBadPageToken = errors.New("store: bad page token")

// eventSource is the source of the events torii writes about users.
const eventSource = "torii"

// Event types about users. Routes live in pkg/bus/routes.go.
const (
	eventUserActivated = "user.activated"
	eventUserUpdated   = "user.updated"
)

// Users reads and writes torii's users table. A change other services care
// about, a sign-up or a new status or time zone, is written to the outbox in
// the same transaction.
type Users struct {
	pool *pgxpool.Pool
	q    *db.Queries
}

// NewUsers returns a Users store that runs on pool.
func NewUsers(pool *pgxpool.Pool) *Users { return &Users{pool: pool, q: db.New(pool)} }

// Upsert creates the user, or refreshes the email and display name of the
// existing one. Role, status, Gmail approval and timezone are kept. A new user
// is announced with user.activated.
func (u *Users) Upsert(ctx context.Context, user domain.User) (domain.User, error) {
	var out domain.User
	err := postgres.InTx(ctx, u.pool, func(tx pgx.Tx) error {
		row, err := db.New(tx).UpsertUser(ctx, db.UpsertUserParams{
			ID:          user.ID,
			GoogleSub:   user.GoogleSub,
			Email:       user.Email,
			DisplayName: user.DisplayName,
		})
		if err != nil {
			return fmt.Errorf("upsert user: %w", err)
		}
		out = toUser(db.User{
			ID: row.ID, GoogleSub: row.GoogleSub, Email: row.Email, DisplayName: row.DisplayName,
			Role: row.Role, Status: row.Status, GmailAllowed: row.GmailAllowed, Timezone: row.Timezone,
			Version: row.Version, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		})
		if !row.Inserted {
			return nil
		}
		return writeUserEvent(ctx, tx, eventUserActivated, out, &toriiv1.UserActivated{
			OwnerId: out.ID.String(), Timezone: out.Timezone, Status: string(out.Status), Version: out.Version,
		})
	})
	if err != nil {
		return domain.User{}, err
	}
	return out, nil
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

// SetStatus enables or disables the user and returns the updated user. The
// change is announced with user.updated.
func (u *Users) SetStatus(ctx context.Context, id uuid.UUID, status domain.Status) (domain.User, error) {
	return u.changeAndAnnounce(ctx, "set user status", func(q *db.Queries) (db.User, error) {
		return q.SetUserStatus(ctx, db.SetUserStatusParams{ID: id, Status: string(status)})
	})
}

// SetGmailAllowed records the admin's Gmail approval and returns the updated user.
func (u *Users) SetGmailAllowed(ctx context.Context, id uuid.UUID, allowed bool) (domain.User, error) {
	row, err := u.q.SetUserGmailAllowed(ctx, db.SetUserGmailAllowedParams{ID: id, GmailAllowed: allowed})
	if err != nil {
		return domain.User{}, notFoundOr(err, "set gmail allowed")
	}
	return toUser(row), nil
}

// SetTimezone changes the zone the user's days are counted in and announces it
// with user.updated.
func (u *Users) SetTimezone(ctx context.Context, id uuid.UUID, timezone string) (domain.User, error) {
	return u.changeAndAnnounce(ctx, "set user timezone", func(q *db.Queries) (db.User, error) {
		return q.SetUserTimezone(ctx, db.SetUserTimezoneParams{ID: id, Timezone: timezone})
	})
}

// changeAndAnnounce runs change and writes user.updated for its result in one
// transaction, so the event exists exactly when the change does.
func (u *Users) changeAndAnnounce(ctx context.Context, what string, change func(*db.Queries) (db.User, error)) (domain.User, error) {
	var out domain.User
	err := postgres.InTx(ctx, u.pool, func(tx pgx.Tx) error {
		row, err := change(db.New(tx))
		if err != nil {
			return notFoundOr(err, what)
		}
		out = toUser(row)
		return writeUserEvent(ctx, tx, eventUserUpdated, out, &toriiv1.UserUpdated{
			OwnerId: out.ID.String(), Timezone: out.Timezone, Status: string(out.Status), Version: out.Version,
		})
	})
	if err != nil {
		return domain.User{}, err
	}
	return out, nil
}

func writeUserEvent(ctx context.Context, tx pgx.Tx, typ string, user domain.User, payload proto.Message) error {
	if _, err := outbox.Write(ctx, tx, eventSource, typ, user.ID.String(), payload); err != nil {
		return fmt.Errorf("write %s: %w", typ, err)
	}
	return nil
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
