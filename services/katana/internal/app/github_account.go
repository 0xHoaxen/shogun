package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/pkg/envelope"
	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
	"github.com/0xHoaxen/shogun/services/katana/internal/store"
	"github.com/0xHoaxen/shogun/services/katana/internal/store/db"
)

// tokenAADPrefix binds a sealed token to the owner it belongs to, so a row
// copied to another user's id will not open.
const tokenAADPrefix = "katana.github_accounts.token/"

// ErrGitHubStorageOff means katana has no master key to seal tokens, so GitHub
// accounts cannot be stored or opened.
var ErrGitHubStorageOff = errors.New("app: github account storage is not configured")

// WithKeyring sets the master keys that seal users' GitHub tokens.
func WithKeyring(keys *envelope.Keyring) Option { return func(s *Service) { s.keys = keys } }

// GitHubAccountView is a user's GitHub connection without its token.
type GitHubAccountView struct {
	Login       string
	ConnectedAt time.Time
}

func tokenAAD(owner uuid.UUID) []byte { return []byte(tokenAADPrefix + owner.String()) }

// SaveGitHubAccount stores the caller's GitHub login and token, sealed,
// replacing any earlier connection. The token is not returned or logged.
func (s *Service) SaveGitHubAccount(ctx context.Context, login, token string) (GitHubAccountView, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return GitHubAccountView{}, err
	}
	if s.keys == nil {
		return GitHubAccountView{}, ErrGitHubStorageOff
	}
	login = strings.TrimSpace(login)
	if login == "" || token == "" {
		return GitHubAccountView{}, fmt.Errorf("%w: login and token are required", domain.ErrNotConnected)
	}
	sealed, keyID, err := s.keys.Seal([]byte(token), tokenAAD(owner))
	if err != nil {
		return GitHubAccountView{}, fmt.Errorf("seal github token: %w", err)
	}
	row, err := store.New(s.pool).SaveGitHubAccount(ctx, db.UpsertGitHubAccountParams{
		OwnerID: owner, Login: login, TokenCiphertext: sealed, KeyID: keyID,
	})
	if err != nil {
		return GitHubAccountView{}, err
	}
	return GitHubAccountView{Login: row.Login, ConnectedAt: row.ConnectedAt}, nil
}

// GitHubAccount returns the caller's GitHub connection without its token, or
// ErrGitHubNotConnected.
func (s *Service) GitHubAccount(ctx context.Context) (GitHubAccountView, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return GitHubAccountView{}, err
	}
	row, err := store.New(s.pool).GitHubAccount(ctx, owner)
	if errors.Is(err, store.ErrNotFound) {
		return GitHubAccountView{}, ErrGitHubNotConnected
	}
	if err != nil {
		return GitHubAccountView{}, err
	}
	return GitHubAccountView{Login: row.Login, ConnectedAt: row.ConnectedAt}, nil
}

// DisconnectGitHub removes the caller's GitHub token. Having none is not an
// error. Snapshots and suggestions already made are kept.
func (s *Service) DisconnectGitHub(ctx context.Context) error {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return err
	}
	_, err = store.New(s.pool).DeleteGitHubAccount(ctx, owner)
	return err
}

// credentialsOf opens the owner's GitHub login and token.
func (s *Service) credentialsOf(ctx context.Context, repo *store.Repo, owner uuid.UUID) (domain.Credentials, error) {
	if s.keys == nil {
		return domain.Credentials{}, ErrGitHubStorageOff
	}
	row, err := repo.GitHubAccount(ctx, owner)
	if errors.Is(err, store.ErrNotFound) {
		return domain.Credentials{}, ErrGitHubNotConnected
	}
	if err != nil {
		return domain.Credentials{}, err
	}
	token, err := s.keys.Open(row.TokenCiphertext, row.KeyID, tokenAAD(owner))
	if err != nil {
		return domain.Credentials{}, fmt.Errorf("open github token: %w", err)
	}
	return domain.Credentials{Login: row.Login, Token: string(token)}, nil
}
