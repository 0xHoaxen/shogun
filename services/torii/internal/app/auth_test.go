package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/domain"
	"github.com/0xHoaxen/shogun/services/torii/internal/store"
)

const (
	ownerEmail = "Owner@Example.com"
	ownerSub   = "google-subject-1"
	sessionTTL = 24 * time.Hour
)

var authNow = time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)

// fakeSessions is an in-memory SessionStore keyed by token hash.
type fakeSessions struct {
	byHash map[string]domain.Session
}

func newFakeSessions() *fakeSessions {
	return &fakeSessions{byHash: map[string]domain.Session{}}
}

func (f *fakeSessions) Insert(_ context.Context, s domain.Session, hash []byte) error {
	f.byHash[string(hash)] = s
	return nil
}

func (f *fakeSessions) ByTokenHash(_ context.Context, hash []byte) (domain.Session, error) {
	s, ok := f.byHash[string(hash)]
	if !ok {
		return domain.Session{}, store.ErrNotFound
	}
	return s, nil
}

func (f *fakeSessions) Extend(_ context.Context, id uuid.UUID, expiresAt time.Time) error {
	for hash, s := range f.byHash {
		if s.ID == id {
			s.ExpiresAt = expiresAt
			f.byHash[hash] = s
			return nil
		}
	}
	return store.ErrNotFound
}

func (f *fakeSessions) Delete(_ context.Context, hash []byte) error {
	delete(f.byHash, string(hash))
	return nil
}

func newAuth(t *testing.T, sessions *fakeSessions, now time.Time) *app.Auth {
	t.Helper()
	auth, err := app.NewAuth(sessions,
		app.AuthConfig{AllowedEmails: []string{" " + ownerEmail + " "}, SessionTTL: sessionTTL},
		app.WithClock(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	return auth
}

func ownerClaims() app.Claims {
	return app.Claims{Subject: ownerSub, Email: "owner@example.com", EmailVerified: true, DisplayName: "Owner"}
}

func TestNewAuthFailsClosed(t *testing.T) {
	tests := []struct {
		name string
		cfg  app.AuthConfig
	}{
		{"empty allowlist", app.AuthConfig{SessionTTL: sessionTTL}},
		{"blank allowlist entry", app.AuthConfig{AllowedEmails: []string{"  "}, SessionTTL: sessionTTL}},
		{"zero ttl", app.AuthConfig{AllowedEmails: []string{ownerEmail}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			_, err := app.NewAuth(newFakeSessions(), tt.cfg)

			// Assert
			if err == nil {
				t.Fatal("NewAuth succeeded, want error")
			}
		})
	}
}

func TestStartSession(t *testing.T) {
	tests := []struct {
		name    string
		claims  app.Claims
		wantErr error
	}{
		{"allowed email", ownerClaims(), nil},
		{"allowed email in other case", app.Claims{Subject: ownerSub, Email: "OWNER@example.COM", EmailVerified: true}, nil},
		{"email not on the allowlist", app.Claims{Subject: ownerSub, Email: "other@example.com", EmailVerified: true}, app.ErrNotAllowed},
		{"allowed email but unverified", app.Claims{Subject: ownerSub, Email: "owner@example.com"}, app.ErrNotAllowed},
		{"missing subject", app.Claims{Email: "owner@example.com", EmailVerified: true}, app.ErrNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			sessions := newFakeSessions()
			auth := newAuth(t, sessions, authNow)

			// Act
			token, session, err := auth.StartSession(context.Background(), tt.claims)

			// Assert
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if token != "" || len(sessions.byHash) != 0 {
					t.Fatalf("rejected login left token %q and %d sessions", token, len(sessions.byHash))
				}
				return
			}
			if token == "" || !session.ExpiresAt.Equal(authNow.Add(sessionTTL)) {
				t.Fatalf("token = %q, expires = %v", token, session.ExpiresAt)
			}
		})
	}
}

func TestStartSessionStoresOnlyTheTokenHash(t *testing.T) {
	// Arrange
	sessions := newFakeSessions()
	auth := newAuth(t, sessions, authNow)

	// Act
	token, _, err := auth.StartSession(context.Background(), ownerClaims())
	// Assert
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if _, stored := sessions.byHash[token]; stored {
		t.Fatal("session stored under the plaintext token")
	}
}

func TestOwnerIDIsStableAcrossLogins(t *testing.T) {
	// Arrange
	auth := newAuth(t, newFakeSessions(), authNow)

	// Act
	_, first, errFirst := auth.StartSession(context.Background(), ownerClaims())
	_, second, errSecond := auth.StartSession(context.Background(), ownerClaims())

	// Assert
	if errFirst != nil || errSecond != nil {
		t.Fatalf("StartSession errors: %v, %v", errFirst, errSecond)
	}
	if first.OwnerID != second.OwnerID || first.ID == second.ID {
		t.Fatalf("owner ids %v/%v, session ids %v/%v", first.OwnerID, second.OwnerID, first.ID, second.ID)
	}
}

func TestAuthenticate(t *testing.T) {
	tests := []struct {
		name        string
		checkAt     time.Time
		token       func(issued string) string
		wantErr     error
		wantRenewed bool
	}{
		{"fresh session", authNow.Add(time.Hour), same, nil, false},
		{"under half left is renewed", authNow.Add(sessionTTL/2 + time.Minute), same, nil, true},
		{"expired session", authNow.Add(sessionTTL), same, app.ErrUnauthenticated, false},
		{"unknown token", authNow, func(string) string { return "nope" }, app.ErrUnauthenticated, false},
		{"empty token", authNow, func(string) string { return "" }, app.ErrUnauthenticated, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			sessions := newFakeSessions()
			token, _, err := newAuth(t, sessions, authNow).StartSession(context.Background(), ownerClaims())
			if err != nil {
				t.Fatalf("StartSession: %v", err)
			}
			checker := newAuth(t, sessions, tt.checkAt)

			// Act
			session, renewed, err := checker.Authenticate(context.Background(), tt.token(token))

			// Assert
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if renewed != tt.wantRenewed {
				t.Fatalf("renewed = %v, want %v", renewed, tt.wantRenewed)
			}
			if tt.wantRenewed && !session.ExpiresAt.Equal(tt.checkAt.Add(sessionTTL)) {
				t.Fatalf("ExpiresAt = %v, want %v", session.ExpiresAt, tt.checkAt.Add(sessionTTL))
			}
		})
	}
}

func TestEndSessionLogsOut(t *testing.T) {
	// Arrange
	sessions := newFakeSessions()
	auth := newAuth(t, sessions, authNow)
	token, _, err := auth.StartSession(context.Background(), ownerClaims())
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	// Act
	endErr := auth.EndSession(context.Background(), token)
	_, _, authErr := auth.Authenticate(context.Background(), token)
	againErr := auth.EndSession(context.Background(), token)

	// Assert
	if endErr != nil || againErr != nil {
		t.Fatalf("EndSession errors: %v, %v", endErr, againErr)
	}
	if !errors.Is(authErr, app.ErrUnauthenticated) {
		t.Fatalf("Authenticate after logout = %v, want ErrUnauthenticated", authErr)
	}
}

func same(token string) string { return token }
