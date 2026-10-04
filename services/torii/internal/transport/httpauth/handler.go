package httpauth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/oauth2"

	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/domain"
)

// Error codes put in the ?error= query of the login page redirect.
const (
	errorNotAllowed   = "not_allowed"
	errorInvalidState = "invalid_state"
	errorDenied       = "denied"
	errorUnavailable  = "unavailable"
)

const stateBytes = 16

// Authenticator starts and ends sessions.
type Authenticator interface {
	StartSession(ctx context.Context, claims app.Claims) (string, domain.Session, error)
	EndSession(ctx context.Context, token string) error
}

// Config configures the login Handler.
type Config struct {
	// LoginURL is where failed logins are sent, with ?error=<code>.
	LoginURL string
	// PostLoginURL is where a successful login is sent.
	PostLoginURL string
	// SecureCookies sets the Secure flag; off only for plain-http local dev.
	SecureCookies bool
	// FlowKey seals the OAuth flow cookie; see DeriveFlowKey. At least 32 bytes.
	FlowKey []byte
	// Now is the clock; nil means time.Now.
	Now func() time.Time
}

// Handler serves /auth/login, /auth/callback and /auth/logout.
type Handler struct {
	provider Provider
	auth     Authenticator
	cfg      Config
	log      *slog.Logger
}

// NewHandler returns a Handler. It rejects a short FlowKey and missing URLs.
func NewHandler(provider Provider, auth Authenticator, cfg Config, log *slog.Logger) (*Handler, error) {
	if len(cfg.FlowKey) < minFlowKeySize {
		return nil, fmt.Errorf("httpauth: flow key must be at least %d bytes", minFlowKeySize)
	}
	if cfg.LoginURL == "" || cfg.PostLoginURL == "" {
		return nil, errors.New("httpauth: login and post-login URLs are required")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Handler{provider: provider, auth: auth, cfg: cfg, log: log}, nil
}

// Routes returns a mux with the three auth routes mounted.
func (h *Handler) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/login", h.login)
	mux.HandleFunc("GET /auth/callback", h.callback)
	mux.HandleFunc("POST /auth/logout", h.logout)
	return mux
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	state, err := randomState()
	if err != nil {
		h.log.ErrorContext(r.Context(), "generate oauth state", slog.Any("error", err))
		h.redirectToLogin(w, r, errorUnavailable)
		return
	}
	verifier := oauth2.GenerateVerifier()
	expires := h.cfg.Now().Add(flowTTL)
	pending := flow{State: state, Verifier: verifier, ExpiresAt: expires.Unix()}
	http.SetCookie(w, flowCookie(h.cfg.FlowKey, pending, h.cfg.SecureCookies))
	http.Redirect(w, r, h.provider.AuthURL(state, verifier), http.StatusFound)
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, clearedFlowCookie(h.cfg.SecureCookies))

	pending, ok := h.pendingFlow(r)
	if !ok || subtle.ConstantTimeCompare([]byte(pending.State), []byte(r.URL.Query().Get("state"))) != 1 {
		h.redirectToLogin(w, r, errorInvalidState)
		return
	}
	code := r.URL.Query().Get("code")
	if r.URL.Query().Get("error") != "" || code == "" {
		h.redirectToLogin(w, r, errorDenied)
		return
	}
	claims, err := h.provider.Exchange(r.Context(), code, pending.Verifier)
	if err != nil {
		h.log.WarnContext(r.Context(), "oauth code exchange failed", slog.Any("error", err))
		h.redirectToLogin(w, r, errorUnavailable)
		return
	}
	token, session, err := h.auth.StartSession(r.Context(), claims)
	if errors.Is(err, app.ErrNotAllowed) {
		h.log.InfoContext(r.Context(), "login rejected: account not allowed")
		h.redirectToLogin(w, r, errorNotAllowed)
		return
	}
	if err != nil {
		h.log.ErrorContext(r.Context(), "start session", slog.Any("error", err))
		h.redirectToLogin(w, r, errorUnavailable)
		return
	}
	http.SetCookie(w, SessionCookie(token, session.ExpiresAt, h.cfg.SecureCookies))
	http.Redirect(w, r, h.cfg.PostLoginURL, http.StatusFound)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(SessionCookieName); err == nil {
		if err := h.auth.EndSession(r.Context(), cookie.Value); err != nil {
			h.log.ErrorContext(r.Context(), "end session", slog.Any("error", err))
			http.Error(w, "logout failed", http.StatusInternalServerError)
			return
		}
	}
	http.SetCookie(w, ClearedSessionCookie(h.cfg.SecureCookies))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) pendingFlow(r *http.Request) (flow, bool) {
	cookie, err := r.Cookie(flowCookieName)
	if err != nil {
		return flow{}, false
	}
	pending, err := openFlow(h.cfg.FlowKey, cookie.Value, h.cfg.Now())
	if err != nil {
		return flow{}, false
	}
	return pending, true
}

func (h *Handler) redirectToLogin(w http.ResponseWriter, r *http.Request, code string) {
	target, err := url.Parse(h.cfg.LoginURL)
	if err != nil {
		http.Error(w, "login unavailable", http.StatusInternalServerError)
		return
	}
	query := target.Query()
	query.Set("error", code)
	target.RawQuery = query.Encode()
	http.Redirect(w, r, target.String(), http.StatusFound)
}

func randomState() (string, error) {
	buf := make([]byte, stateBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("read random bytes: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
