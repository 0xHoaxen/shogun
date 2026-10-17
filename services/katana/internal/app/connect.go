package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
)

const (
	// stateTTL is how long a user has to finish granting access on GitHub.
	stateTTL = 10 * time.Minute
	// stateAAD binds a sealed state to this purpose, so a sealed token or any
	// other sealed value cannot be replayed as a state.
	stateAAD = "katana:oauth-state"
	// stateSep splits the key id from the sealed bytes in a state.
	stateSep = "."
	// nonceBytes of randomness make every state differ even for the same owner.
	nonceBytes = 16
)

// Errors from connecting GitHub.
var (
	// ErrInvalidState means the callback state is forged, expired or another user's.
	ErrInvalidState = errors.New("app: github connect state is not valid")
	// ErrInvalidCode means GitHub refused the authorization code, or there was none.
	ErrInvalidCode = errors.New("app: github authorization code is not valid")
)

// OAuth is GitHub's authorization-code flow.
type OAuth interface {
	AuthURL(state string) string
	Exchange(ctx context.Context, code string) (token string, err error)
	Login(ctx context.Context, token string) (login string, err error)
}

// WithOAuth sets the GitHub OAuth app users connect through.
func WithOAuth(oauth OAuth) Option { return func(s *Service) { s.oauth = oauth } }

// pending is what a state remembers between the two steps.
type pending struct {
	Owner     uuid.UUID `json:"o"`
	Nonce     string    `json:"n"`
	ExpiresAt int64     `json:"e"`
}

// ConnectGitHub returns where to send the caller to grant read access on
// GitHub. The owner and expiry travel in the state, sealed, so katana keeps
// nothing between the two steps and a state can be neither read nor forged.
func (s *Service) ConnectGitHub(ctx context.Context) (string, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return "", err
	}
	if s.oauth == nil {
		return "", ErrGitHubNotConfigured
	}
	if s.keys == nil {
		return "", ErrGitHubStorageOff
	}
	nonce := make([]byte, nonceBytes)
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("state nonce: %w", err)
	}
	raw, err := json.Marshal(pending{
		Owner: owner, Nonce: base64.RawURLEncoding.EncodeToString(nonce), ExpiresAt: s.now().Add(stateTTL).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("encode state: %w", err)
	}
	sealed, keyID, err := s.keys.Seal(raw, []byte(stateAAD))
	if err != nil {
		return "", fmt.Errorf("seal state: %w", err)
	}
	return s.oauth.AuthURL(keyID + stateSep + base64.RawURLEncoding.EncodeToString(sealed)), nil
}

// CompleteGitHubConnect finishes the connection: it checks the state belongs to
// the caller and is fresh, trades the code for a token, learns the login, and
// stores both sealed. Nothing is stored unless every step succeeds.
func (s *Service) CompleteGitHubConnect(ctx context.Context, code, state string) (GitHubAccountView, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return GitHubAccountView{}, err
	}
	if s.oauth == nil {
		return GitHubAccountView{}, ErrGitHubNotConfigured
	}
	if s.keys == nil {
		return GitHubAccountView{}, ErrGitHubStorageOff
	}
	p, err := s.openState(state)
	if err != nil || p.Owner != owner || s.now().Unix() > p.ExpiresAt {
		return GitHubAccountView{}, ErrInvalidState
	}
	if strings.TrimSpace(code) == "" {
		return GitHubAccountView{}, ErrInvalidCode
	}
	token, err := s.oauth.Exchange(ctx, code)
	if errors.Is(err, domain.ErrCodeRefused) {
		return GitHubAccountView{}, ErrInvalidCode
	}
	if err != nil {
		return GitHubAccountView{}, wrap("exchange github code", err)
	}
	login, err := s.oauth.Login(ctx, token)
	if err != nil {
		return GitHubAccountView{}, wrap("read github login", err)
	}
	return s.SaveGitHubAccount(ctx, login, token)
}

func (s *Service) openState(state string) (pending, error) {
	i := strings.LastIndex(state, stateSep)
	if i < 1 {
		return pending{}, ErrInvalidState
	}
	sealed, err := base64.RawURLEncoding.DecodeString(state[i+1:])
	if err != nil || len(sealed) == 0 {
		return pending{}, ErrInvalidState
	}
	raw, err := s.keys.Open(sealed, state[:i], []byte(stateAAD))
	if err != nil {
		return pending{}, ErrInvalidState
	}
	var p pending
	if err := json.Unmarshal(raw, &p); err != nil {
		return pending{}, ErrInvalidState
	}
	return p, nil
}
