package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/torii/internal/domain"
	"github.com/0xHoaxen/shogun/services/torii/internal/store/db"
)

// ErrNotFound is returned when no session matches.
var ErrNotFound = errors.New("store: not found")

// DBTX is a pool or a transaction.
type DBTX = db.DBTX

// Sessions reads and writes the torii sessions table.
type Sessions struct {
	q *db.Queries
}

// NewSessions returns a Sessions store that runs on d.
func NewSessions(d DBTX) *Sessions {
	return &Sessions{q: db.New(d)}
}

// NewID returns a new UUIDv7.
func NewID() uuid.UUID {
	return uuid.Must(uuid.NewV7())
}

// Insert stores a session under the SHA-256 of its token.
func (s *Sessions) Insert(ctx context.Context, session domain.Session, tokenHash []byte) error {
	err := s.q.InsertSession(ctx, db.InsertSessionParams{
		ID:          session.ID,
		OwnerID:     session.OwnerID,
		Email:       session.Email,
		DisplayName: session.DisplayName,
		TokenHash:   tokenHash,
		ExpiresAt:   session.ExpiresAt,
	})
	if err != nil {
		return fmt.Errorf("insert session: %w", err)
	}
	return nil
}

// ByTokenHash returns the session stored under tokenHash, or ErrNotFound.
func (s *Sessions) ByTokenHash(ctx context.Context, tokenHash []byte) (domain.Session, error) {
	row, err := s.q.GetSessionByTokenHash(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Session{}, ErrNotFound
	}
	if err != nil {
		return domain.Session{}, fmt.Errorf("get session: %w", err)
	}
	return domain.Session{
		ID:          row.ID,
		OwnerID:     row.OwnerID,
		Email:       row.Email,
		DisplayName: row.DisplayName,
		ExpiresAt:   row.ExpiresAt,
	}, nil
}

// Extend moves a session's expiry, or returns ErrNotFound if it is gone.
func (s *Sessions) Extend(ctx context.Context, id uuid.UUID, expiresAt time.Time) error {
	n, err := s.q.ExtendSession(ctx, db.ExtendSessionParams{ID: id, ExpiresAt: expiresAt})
	if err != nil {
		return fmt.Errorf("extend session: %w", err)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// Delete removes the session stored under tokenHash. A missing session is not
// an error, so logging out twice is harmless.
func (s *Sessions) Delete(ctx context.Context, tokenHash []byte) error {
	if err := s.q.DeleteSessionByTokenHash(ctx, tokenHash); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// DeleteExpired removes sessions that expired at or before now and returns
// how many it removed.
func (s *Sessions) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	n, err := s.q.DeleteExpiredSessions(ctx, now)
	if err != nil {
		return 0, fmt.Errorf("delete expired sessions: %w", err)
	}
	return n, nil
}
