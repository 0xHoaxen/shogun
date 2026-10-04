package httpauth_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/domain"
	"github.com/0xHoaxen/shogun/services/torii/internal/idp/idptest"
	"github.com/0xHoaxen/shogun/services/torii/internal/store"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/httpauth"
)

const (
	clientID     = "shogun-client"
	clientSecret = "shogun-secret"
	redirectURL  = "http://torii.test/auth/callback"
	loginURL     = "/login"
	postLogin    = "/jobs"
	ownerEmail   = "owner@example.com"
	sessionTTL   = 24 * time.Hour
)

var identityKey = []byte("0123456789abcdef0123456789abcdef")

// memSessions is an in-memory app.SessionStore keyed by token hash.
type memSessions struct {
	byHash map[string]domain.Session
}

func (m *memSessions) Insert(_ context.Context, s domain.Session, hash []byte) error {
	m.byHash[string(hash)] = s
	return nil
}

func (m *memSessions) ByTokenHash(_ context.Context, hash []byte) (domain.Session, error) {
	s, ok := m.byHash[string(hash)]
	if !ok {
		return domain.Session{}, store.ErrNotFound
	}
	return s, nil
}

func (m *memSessions) Extend(context.Context, uuid.UUID, time.Time) error { return nil }

func (m *memSessions) Delete(_ context.Context, hash []byte) error {
	delete(m.byHash, string(hash))
	return nil
}

type harness struct {
	idp      *idptest.IDP
	sessions *memSessions
	auth     *app.Auth
	routes   http.Handler
	now      *time.Time
}

func newHarness(t *testing.T, secure bool) *harness {
	t.Helper()
	idp := idptest.New(t, clientID, clientSecret)
	provider, err := httpauth.NewOIDCProvider(context.Background(), httpauth.ProviderConfig{
		IssuerURL: idp.IssuerURL(), ClientID: clientID, ClientSecret: clientSecret, RedirectURL: redirectURL,
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	sessions := &memSessions{byHash: map[string]domain.Session{}}
	auth, err := app.NewAuth(sessions, app.AuthConfig{AllowedEmails: []string{ownerEmail}, SessionTTL: sessionTTL})
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	now := time.Now()
	handler, err := httpauth.NewHandler(provider, auth, httpauth.Config{
		LoginURL: loginURL, PostLoginURL: postLogin, SecureCookies: secure,
		FlowKey: httpauth.DeriveFlowKey(identityKey), Now: func() time.Time { return now },
	}, slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewHandler: %v", err)
	}
	return &harness{idp: idp, sessions: sessions, auth: auth, routes: handler.Routes(), now: &now}
}

func (h *harness) do(method, target string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	h.routes.ServeHTTP(rec, req)
	return rec
}

// startLogin hits /auth/login and returns what the browser would hold: the
// flow cookie, the state and the PKCE challenge from the provider redirect.
func (h *harness) startLogin(t *testing.T) (flowCookie *http.Cookie, state, challenge string) {
	t.Helper()
	rec := h.do(http.MethodGet, "/auth/login")
	if rec.Code != http.StatusFound {
		t.Fatalf("login status = %d, want 302", rec.Code)
	}
	location, err := url.Parse(rec.Header().Get("Location"))
	if err != nil {
		t.Fatalf("parse login redirect: %v", err)
	}
	query := location.Query()
	if query.Get("code_challenge_method") != "S256" || query.Get("client_id") != clientID {
		t.Fatalf("provider redirect lacks PKCE S256 or client id: %v", query)
	}
	cookies := rec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login set %d cookies, want the flow cookie only", len(cookies))
	}
	return cookies[0], query.Get("state"), query.Get("code_challenge")
}

func (h *harness) callback(code, state string, flow *http.Cookie) *httptest.ResponseRecorder {
	target := "/auth/callback?" + url.Values{"code": {code}, "state": {state}}.Encode()
	return h.do(http.MethodGet, target, flow)
}

func ownerClaims() idptest.Claims {
	return idptest.Claims{Subject: "sub-1", Email: ownerEmail, EmailVerified: true, Name: "Owner"}
}

func sessionCookie(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == httpauth.SessionCookieName {
			return c
		}
	}
	return nil
}

func wantRedirect(t *testing.T, rec *httptest.ResponseRecorder, want string) {
	t.Helper()
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != want {
		t.Fatalf("response = %d %q, want 302 %q", rec.Code, rec.Header().Get("Location"), want)
	}
}

func TestLoginWithAllowedEmailStartsSession(t *testing.T) {
	// Arrange
	h := newHarness(t, true)
	flow, state, challenge := h.startLogin(t)
	claims := ownerClaims()
	claims.Email = "Owner@Example.com"
	code := h.idp.IssueCode(challenge, claims)

	// Act
	rec := h.callback(code, state, flow)

	// Assert
	wantRedirect(t, rec, postLogin)
	cookie := sessionCookie(rec)
	if cookie == nil || !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie = %+v, want HttpOnly Secure SameSite=Lax", cookie)
	}
	if _, _, err := h.auth.Authenticate(context.Background(), cookie.Value); err != nil {
		t.Fatalf("session cookie does not authenticate: %v", err)
	}
}

func TestLoginCookieIsNotSecureForLocalHTTP(t *testing.T) {
	// Arrange
	h := newHarness(t, false)
	flow, state, challenge := h.startLogin(t)
	code := h.idp.IssueCode(challenge, ownerClaims())

	// Act
	cookie := sessionCookie(h.callback(code, state, flow))

	// Assert
	if cookie == nil || cookie.Secure {
		t.Fatalf("session cookie = %+v, want present and not Secure", cookie)
	}
}

func TestLoginWithDisallowedEmailIsRejected(t *testing.T) {
	// Arrange
	h := newHarness(t, true)
	flow, state, challenge := h.startLogin(t)
	claims := ownerClaims()
	claims.Email = "stranger@example.com"
	code := h.idp.IssueCode(challenge, claims)

	// Act
	rec := h.callback(code, state, flow)

	// Assert
	wantRedirect(t, rec, loginURL+"?error=not_allowed")
	if sessionCookie(rec) != nil || len(h.sessions.byHash) != 0 {
		t.Fatalf("rejected login left a cookie or %d sessions", len(h.sessions.byHash))
	}
}

func TestLoginWithUnverifiedEmailIsRejected(t *testing.T) {
	// Arrange
	h := newHarness(t, true)
	flow, state, challenge := h.startLogin(t)
	claims := ownerClaims()
	claims.EmailVerified = false
	code := h.idp.IssueCode(challenge, claims)

	// Act
	rec := h.callback(code, state, flow)

	// Assert
	wantRedirect(t, rec, loginURL+"?error=not_allowed")
}

func TestCallbackRejectsBadFlow(t *testing.T) {
	tests := []struct {
		name  string
		build func(t *testing.T, h *harness) (code, state string, flow *http.Cookie)
	}{
		{"state does not match", func(t *testing.T, h *harness) (string, string, *http.Cookie) {
			flow, _, challenge := h.startLogin(t)
			return h.idp.IssueCode(challenge, ownerClaims()), "forged", flow
		}},
		{"flow cookie missing", func(t *testing.T, h *harness) (string, string, *http.Cookie) {
			_, state, challenge := h.startLogin(t)
			return h.idp.IssueCode(challenge, ownerClaims()), state, nil
		}},
		{"flow cookie tampered", func(t *testing.T, h *harness) (string, string, *http.Cookie) {
			flow, state, challenge := h.startLogin(t)
			tampered := *flow
			tampered.Value = "x" + flow.Value
			return h.idp.IssueCode(challenge, ownerClaims()), state, &tampered
		}},
		{"flow cookie expired", func(t *testing.T, h *harness) (string, string, *http.Cookie) {
			flow, state, challenge := h.startLogin(t)
			*h.now = h.now.Add(time.Hour)
			return h.idp.IssueCode(challenge, ownerClaims()), state, flow
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			h := newHarness(t, true)
			code, state, flow := tt.build(t, h)
			var cookies []*http.Cookie
			if flow != nil {
				cookies = append(cookies, flow)
			}

			// Act
			rec := h.do(http.MethodGet, "/auth/callback?"+url.Values{"code": {code}, "state": {state}}.Encode(), cookies...)

			// Assert
			wantRedirect(t, rec, loginURL+"?error=invalid_state")
			if len(h.sessions.byHash) != 0 {
				t.Fatalf("bad flow created %d sessions", len(h.sessions.byHash))
			}
		})
	}
}

func TestCallbackWithProviderErrorIsDenied(t *testing.T) {
	// Arrange
	h := newHarness(t, true)
	flow, state, _ := h.startLogin(t)

	// Act
	rec := h.do(http.MethodGet, "/auth/callback?"+url.Values{"error": {"access_denied"}, "state": {state}}.Encode(), flow)

	// Assert
	wantRedirect(t, rec, loginURL+"?error=denied")
}

func TestCodeCannotBeReplayed(t *testing.T) {
	// Arrange
	h := newHarness(t, true)
	flow, state, challenge := h.startLogin(t)
	code := h.idp.IssueCode(challenge, ownerClaims())
	wantRedirect(t, h.callback(code, state, flow), postLogin)

	// Act
	rec := h.callback(code, state, flow)

	// Assert
	wantRedirect(t, rec, loginURL+"?error=unavailable")
}

func TestLogoutEndsSessionAndClearsCookie(t *testing.T) {
	// Arrange
	h := newHarness(t, true)
	flow, state, challenge := h.startLogin(t)
	code := h.idp.IssueCode(challenge, ownerClaims())
	session := sessionCookie(h.callback(code, state, flow))

	// Act
	rec := h.do(http.MethodPost, "/auth/logout", session)
	_, _, authErr := h.auth.Authenticate(context.Background(), session.Value)

	// Assert
	if rec.Code != http.StatusNoContent {
		t.Fatalf("logout status = %d, want 204", rec.Code)
	}
	if cleared := sessionCookie(rec); cleared == nil || cleared.MaxAge >= 0 {
		t.Fatalf("logout cookie = %+v, want cleared", cleared)
	}
	if authErr == nil {
		t.Fatal("session still authenticates after logout")
	}
}

func TestLogoutRequiresPost(t *testing.T) {
	// Arrange
	h := newHarness(t, true)

	// Act
	rec := h.do(http.MethodGet, "/auth/logout")

	// Assert
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /auth/logout status = %d, want 405", rec.Code)
	}
}

func TestNewHandlerRejectsBadConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  httpauth.Config
	}{
		{"short flow key", httpauth.Config{LoginURL: loginURL, PostLoginURL: postLogin, FlowKey: []byte("short")}},
		{"missing urls", httpauth.Config{FlowKey: httpauth.DeriveFlowKey(identityKey)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act
			_, err := httpauth.NewHandler(nil, nil, tt.cfg, slog.New(slog.DiscardHandler))

			// Assert
			if err == nil {
				t.Fatal("NewHandler succeeded, want error")
			}
		})
	}
}
