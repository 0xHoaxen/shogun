package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/torii/internal/domain"
	"github.com/0xHoaxen/shogun/services/torii/internal/store"
)

// Errors the transport layer maps to HTTP and Connect codes.
var (
	// ErrNotAllowed means the signed-in Google account is not on the allowlist
	// or its email is not verified.
	ErrNotAllowed = errors.New("app: account not allowed")
	// ErrUnauthenticated means the session token is missing, unknown or expired.
	ErrUnauthenticated = errors.New("app: unauthenticated")
)

const tokenBytes = 32

// ownerNamespace derives a stable owner id from the Google subject, so the
// same account always owns the same data without an owners table.
var ownerNamespace = uuid.MustParse("6f1d7a52-3c4e-5b8a-9d20-1a7e4c9b5f03")

// SessionStore is the persistence the auth use cases need.
type SessionStore interface {
	Insert(ctx context.Context, session domain.Session, tokenHash []byte) error
	ByTokenHash(ctx context.Context, tokenHash []byte) (domain.Session, error)
	Extend(ctx context.Context, id uuid.UUID, expiresAt time.Time) error
	Delete(ctx context.Context, tokenHash []byte) error
}

// Claims is what the identity provider vouches for after a successful login.
type Claims struct {
	Subject       string
	Email         string
	EmailVerified bool
	DisplayName   string
}

// AuthConfig configures the auth use cases.
type AuthConfig struct {
	// AllowedEmails is the owner allowlist. It must not be empty.
	AllowedEmails []string
	// SessionTTL is how long a session lasts, and the sliding renewal window.
	SessionTTL time.Duration
}

// Auth starts, checks and ends sessions.
type Auth struct {
	store   SessionStore
	allowed map[string]struct{}
	ttl     time.Duration
	now     func() time.Time
}

// AuthOption customises an Auth.
type AuthOption func(Auth) Auth

// WithClock replaces the clock, for tests.
func WithClock(now func() time.Time) AuthOption {
	return func(a Auth) Auth {
		a.now = now
		return a
	}
}

// NewAuth returns an Auth. It fails closed on an empty allowlist or a
// non-positive TTL.
func NewAuth(sessions SessionStore, cfg AuthConfig, opts ...AuthOption) (*Auth, error) {
	allowed := make(map[string]struct{}, len(cfg.AllowedEmails))
	for _, email := range cfg.AllowedEmails {
		if normalised := normaliseEmail(email); normalised != "" {
			allowed[normalised] = struct{}{}
		}
	}
	if len(allowed) == 0 {
		return nil, errors.New("app: allowlist is empty")
	}
	if cfg.SessionTTL <= 0 {
		return nil, errors.New("app: session TTL must be positive")
	}
	a := Auth{store: sessions, allowed: allowed, ttl: cfg.SessionTTL, now: time.Now}
	for _, opt := range opts {
		a = opt(a)
	}
	return &a, nil
}

// StartSession checks claims against the allowlist and, if they pass, creates
// a session. It returns the plaintext token to put in the cookie and the
// session; only the token's hash is stored.
func (a *Auth) StartSession(ctx context.Context, claims Claims) (string, domain.Session, error) {
	if !claims.EmailVerified || claims.Subject == "" {
		return "", domain.Session{}, ErrNotAllowed
	}
	email := normaliseEmail(claims.Email)
	if _, ok := a.allowed[email]; !ok {
		return "", domain.Session{}, ErrNotAllowed
	}
	token, err := newToken()
	if err != nil {
		return "", domain.Session{}, err
	}
	session := domain.Session{
		ID:          store.NewID(),
		OwnerID:     uuid.NewSHA1(ownerNamespace, []byte("google:"+claims.Subject)),
		Email:       email,
		DisplayName: claims.DisplayName,
		ExpiresAt:   a.now().Add(a.ttl),
	}
	if err := a.store.Insert(ctx, session, hashToken(token)); err != nil {
		return "", domain.Session{}, err
	}
	return token, session, nil
}

// Authenticate resolves a session token. The bool reports whether the expiry
// was pushed out (sliding renewal), so the caller can refresh the cookie.
func (a *Auth) Authenticate(ctx context.Context, token string) (domain.Session, bool, error) {
	if token == "" {
		return domain.Session{}, false, ErrUnauthenticated
	}
	session, err := a.store.ByTokenHash(ctx, hashToken(token))
	if errors.Is(err, store.ErrNotFound) {
		return domain.Session{}, false, ErrUnauthenticated
	}
	if err != nil {
		return domain.Session{}, false, err
	}
	now := a.now()
	if session.Expired(now) {
		return domain.Session{}, false, ErrUnauthenticated
	}
	renewed, ok := session.Renewed(now, a.ttl)
	if !ok {
		return session, false, nil
	}
	if err := a.store.Extend(ctx, renewed.ID, renewed.ExpiresAt); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return domain.Session{}, false, ErrUnauthenticated
		}
		return domain.Session{}, false, err
	}
	return renewed, true, nil
}

// EndSession deletes the session behind token. An unknown token is not an error.
func (a *Auth) EndSession(ctx context.Context, token string) error {
	if token == "" {
		return nil
	}
	return a.store.Delete(ctx, hashToken(token))
}

func normaliseEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

func newToken() (string, error) {
	buf := make([]byte, tokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
