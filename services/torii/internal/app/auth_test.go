package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/app/apptest"
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

func (f *fakeSessions) DeleteByOwner(_ context.Context, owner uuid.UUID) (int64, error) {
	var n int64
	for hash, s := range f.byHash {
		if s.OwnerID == owner {
			delete(f.byHash, hash)
			n++
		}
	}
	return n, nil
}

func newAuth(t *testing.T, sessions *fakeSessions, users *apptest.MemUsers, now time.Time) *app.Auth {
	t.Helper()
	auth, err := app.NewAuth(sessions, users,
		app.AuthConfig{AdminEmails: []string{" " + ownerEmail + " "}, SessionTTL: sessionTTL},
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
		{"no admins", app.AuthConfig{SessionTTL: sessionTTL}},
		{"blank admin entry", app.AuthConfig{AdminEmails: []string{"  "}, SessionTTL: sessionTTL}},
		{"zero ttl", app.AuthConfig{AdminEmails: []string{ownerEmail}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			_, err := app.NewAuth(newFakeSessions(), apptest.NewMemUsers(), tt.cfg)

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
		{"admin email", ownerClaims(), nil},
		{"admin email in other case", app.Claims{Subject: ownerSub, Email: "OWNER@example.COM", EmailVerified: true}, nil},
		{"any other verified email", app.Claims{Subject: "google-subject-2", Email: "stranger@example.com", EmailVerified: true}, nil},
		{"unverified email", app.Claims{Subject: ownerSub, Email: "owner@example.com"}, app.ErrNotAllowed},
		{"missing subject", app.Claims{Email: "owner@example.com", EmailVerified: true}, app.ErrNotAllowed},
		{"missing email", app.Claims{Subject: ownerSub, EmailVerified: true}, app.ErrNotAllowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			sessions := newFakeSessions()
			auth := newAuth(t, sessions, apptest.NewMemUsers(), authNow)

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
	auth := newAuth(t, sessions, apptest.NewMemUsers(), authNow)

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
	auth := newAuth(t, newFakeSessions(), apptest.NewMemUsers(), authNow)

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
			users := apptest.NewMemUsers()
			token, _, err := newAuth(t, sessions, users, authNow).StartSession(context.Background(), ownerClaims())
			if err != nil {
				t.Fatalf("StartSession: %v", err)
			}
			checker := newAuth(t, sessions, users, tt.checkAt)

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
	auth := newAuth(t, sessions, apptest.NewMemUsers(), authNow)
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

func strangerClaims() app.Claims {
	return app.Claims{Subject: "google-subject-2", Email: "Stranger@Example.com", EmailVerified: true, DisplayName: "Stranger"}
}

func TestSignUpIsOpenAndStrangersAreMembers(t *testing.T) {
	// Arrange
	users := apptest.NewMemUsers()
	auth := newAuth(t, newFakeSessions(), users, authNow)

	// Act
	_, session, err := auth.StartSession(context.Background(), strangerClaims())
	// Assert
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	user, err := users.ByID(context.Background(), session.OwnerID)
	if err != nil {
		t.Fatalf("user not created: %v", err)
	}
	if user.Role != domain.RoleMember || user.GmailAllowed || user.Email != "stranger@example.com" {
		t.Fatalf("user = %+v, want a member without Gmail approval and a normalised email", user)
	}
	if session.Role != domain.RoleMember {
		t.Fatalf("session role = %q, want member", session.Role)
	}
}

func TestAdminEmailsBootstrapAdminsWithGmailApproval(t *testing.T) {
	// Arrange
	users := apptest.NewMemUsers()
	auth := newAuth(t, newFakeSessions(), users, authNow)

	// Act
	_, session, err := auth.StartSession(context.Background(), ownerClaims())
	// Assert
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	user, _ := users.ByID(context.Background(), session.OwnerID)
	if user.Role != domain.RoleAdmin || !user.GmailAllowed || session.Role != domain.RoleAdmin {
		t.Fatalf("user %+v, session role %q, want an admin with Gmail approval", user, session.Role)
	}
}

func TestDisabledUserCannotStartASession(t *testing.T) {
	// Arrange
	users := apptest.NewMemUsers()
	sessions := newFakeSessions()
	auth := newAuth(t, sessions, users, authNow)
	_, first, err := auth.StartSession(context.Background(), strangerClaims())
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if _, err := users.SetStatus(context.Background(), first.OwnerID, domain.StatusDisabled); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	before := len(sessions.byHash)

	// Act
	token, _, err := auth.StartSession(context.Background(), strangerClaims())

	// Assert
	if !errors.Is(err, app.ErrNotAllowed) || token != "" || len(sessions.byHash) != before {
		t.Fatalf("err = %v, token %q, sessions %d->%d, want ErrNotAllowed and no new session", err, token, before, len(sessions.byHash))
	}
}

func TestDisablingAUserEndsTheirSessions(t *testing.T) {
	// Arrange
	users := apptest.NewMemUsers()
	sessions := newFakeSessions()
	auth := newAuth(t, sessions, users, authNow)
	token, session, err := auth.StartSession(context.Background(), strangerClaims())
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if _, err := users.SetStatus(context.Background(), session.OwnerID, domain.StatusDisabled); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	// Act
	_, _, err = auth.Authenticate(context.Background(), token)

	// Assert
	if !errors.Is(err, app.ErrUnauthenticated) {
		t.Fatalf("err = %v, want ErrUnauthenticated", err)
	}
	if len(sessions.byHash) != 0 {
		t.Fatalf("%d sessions left after the user was disabled, want 0", len(sessions.byHash))
	}
}

func TestAuthenticateReportsTheCurrentRole(t *testing.T) {
	// Arrange
	users := apptest.NewMemUsers()
	auth := newAuth(t, newFakeSessions(), users, authNow)
	token, session, err := auth.StartSession(context.Background(), strangerClaims())
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if _, err := users.SetRole(context.Background(), session.OwnerID, domain.RoleAdmin); err != nil {
		t.Fatalf("SetRole: %v", err)
	}

	// Act
	got, _, err := auth.Authenticate(context.Background(), token)

	// Assert
	if err != nil || got.Role != domain.RoleAdmin {
		t.Fatalf("role = %q, err = %v, want admin without a new login", got.Role, err)
	}
}
