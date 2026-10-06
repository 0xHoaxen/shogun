package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/tsubame/internal/envelope"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store/db"
)

const (
	// stateTTL is how long the owner has to grant access.
	stateTTL = 10 * time.Minute
	// verifierBytes makes a 43-character PKCE verifier, the shortest allowed.
	verifierBytes = 32

	stateAAD = "tsubame:oauth-state"
	// stateSep splits the master key id from the sealed state.
	stateSep = "."
)

// Errors CompleteConnect returns for what is wrong with the request.
var (
	// ErrInvalidProvider means the provider is missing or not supported.
	ErrInvalidProvider = errors.New("app: unsupported mail provider")
	// ErrInvalidState means the state is not one this service issued to this
	// owner within the last ten minutes.
	ErrInvalidState = errors.New("app: invalid or expired state")
	// ErrInvalidCode means the provider refused the authorization code.
	ErrInvalidCode = errors.New("app: authorization code refused")
)

// OAuth is the provider's authorization-code flow with PKCE.
type OAuth interface {
	AuthURL(state, verifier string) string
	Exchange(ctx context.Context, code, verifier string) (refreshToken string, err error)
}

// Connector connects mail accounts: it sends the owner to the provider and
// finishes the connection when they come back.
type Connector struct {
	accounts *Accounts
	oauth    OAuth
	factory  mail.Factory
	keys     *envelope.Keyring
	log      *slog.Logger
	now      func() time.Time
}

// NewConnector returns a Connector. A nil now means time.Now.
func NewConnector(accounts *Accounts, oauth OAuth, factory mail.Factory, keys *envelope.Keyring, log *slog.Logger, now func() time.Time) *Connector {
	if now == nil {
		now = time.Now
	}
	return &Connector{accounts: accounts, oauth: oauth, factory: factory, keys: keys, log: log, now: now}
}

// pending is what the state remembers between the two steps.
type pending struct {
	Owner     uuid.UUID `json:"o"`
	Provider  string    `json:"p"`
	Verifier  string    `json:"v"`
	ExpiresAt int64     `json:"e"`
}

// Begin returns the URL that sends the owner to the provider. The PKCE
// verifier and the owner travel in the state, sealed, so the service keeps
// nothing between the two steps and a state cannot be forged or read.
func (c *Connector) Begin(ctx context.Context, provider string) (string, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return "", err
	}
	if provider != ProviderGmail {
		return "", ErrInvalidProvider
	}
	raw := make([]byte, verifierBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("verifier: %w", err)
	}
	verifier := base64.RawURLEncoding.EncodeToString(raw)
	state, err := c.sealState(pending{Owner: owner, Provider: provider, Verifier: verifier, ExpiresAt: c.now().Add(stateTTL).Unix()})
	if err != nil {
		return "", err
	}
	return c.oauth.AuthURL(state, verifier), nil
}

// Complete exchanges the code for a refresh token, reads the mailbox address
// from the provider, and stores the account. The state must be the one Begin
// issued to this same owner.
func (c *Connector) Complete(ctx context.Context, code, state string) (db.Account, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return db.Account{}, err
	}
	p, err := c.openState(state)
	if err != nil || p.Owner != owner || c.now().Unix() > p.ExpiresAt {
		return db.Account{}, ErrInvalidState
	}
	if code == "" {
		return db.Account{}, ErrInvalidCode
	}
	token, err := c.oauth.Exchange(ctx, code, p.Verifier)
	if err != nil {
		if errors.Is(err, mail.ErrAuthRevoked) {
			return db.Account{}, ErrInvalidCode
		}
		return db.Account{}, fmt.Errorf("exchange code: %w", err)
	}
	profile, err := c.factory.Provider(token).Profile(ctx)
	if err != nil {
		return db.Account{}, fmt.Errorf("read mailbox profile: %w", err)
	}
	acc, err := c.accounts.Connect(ctx, owner, p.Provider, profile.Address, token)
	if err != nil {
		return db.Account{}, err
	}
	c.log.Info("mail account connected", slog.String("account_id", acc.ID.String()), slog.String("provider", acc.Provider))
	return acc, nil
}

func (c *Connector) sealState(p pending) (string, error) {
	raw, err := json.Marshal(p)
	if err != nil {
		return "", fmt.Errorf("encode state: %w", err)
	}
	sealed, keyID, err := c.keys.Seal(raw, []byte(stateAAD))
	if err != nil {
		return "", fmt.Errorf("seal state: %w", err)
	}
	return keyID + stateSep + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (c *Connector) openState(state string) (pending, error) {
	i := strings.LastIndex(state, stateSep)
	if i < 1 {
		return pending{}, ErrInvalidState
	}
	sealed, err := base64.RawURLEncoding.DecodeString(state[i+1:])
	if err != nil {
		return pending{}, ErrInvalidState
	}
	raw, err := c.keys.Open(sealed, state[:i], []byte(stateAAD))
	if err != nil {
		return pending{}, ErrInvalidState
	}
	var p pending
	if err := json.Unmarshal(raw, &p); err != nil {
		return pending{}, ErrInvalidState
	}
	return p, nil
}
