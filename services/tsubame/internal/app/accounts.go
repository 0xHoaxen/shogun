// Package app holds the tsubame use cases.
package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/services/tsubame/internal/envelope"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/store/db"
)

// ProviderGmail is the only mail provider so far.
const ProviderGmail = "gmail"

// ErrAccountNotUsable means an account cannot be used until the owner connects
// it again, or turns it back on.
var ErrAccountNotUsable = errors.New("app: account needs to be reconnected")

// Accounts stores connected mail accounts. OAuth refresh tokens are sealed with
// envelope encryption and are only ever opened by Token.
type Accounts struct {
	pool *pgxpool.Pool
	keys *envelope.Keyring
}

// NewAccounts returns Accounts on pool that seal tokens with keys.
func NewAccounts(pool *pgxpool.Pool, keys *envelope.Keyring) *Accounts {
	return &Accounts{pool: pool, keys: keys}
}

// tokenAAD binds a sealed token to its account, so it cannot be copied to
// another row.
func tokenAAD(id uuid.UUID) []byte { return []byte("account:" + id.String()) }

// Connect stores an account with its refresh token, or replaces the token of
// an address connected before.
func (a *Accounts) Connect(ctx context.Context, owner uuid.UUID, provider, address, refreshToken string) (db.Account, error) {
	address = strings.ToLower(strings.TrimSpace(address))
	if provider != ProviderGmail || address == "" || refreshToken == "" {
		return db.Account{}, errors.New("app: provider, address and refresh token are required")
	}
	repo := store.New(a.pool)
	// The id is part of what the token is sealed for, so a reconnect must seal
	// for the id the row already has.
	id, err := a.idFor(ctx, repo, owner, provider, address)
	if err != nil {
		return db.Account{}, err
	}
	sealed, keyID, err := a.keys.Seal([]byte(refreshToken), tokenAAD(id))
	if err != nil {
		return db.Account{}, fmt.Errorf("seal token: %w", err)
	}
	return repo.UpsertAccount(ctx, db.UpsertAccountParams{
		ID: id, OwnerID: owner, Provider: provider, Address: address, TokenCiphertext: sealed, TokenKeyID: keyID,
	})
}

func (a *Accounts) idFor(ctx context.Context, repo *store.Repo, owner uuid.UUID, provider, address string) (uuid.UUID, error) {
	existing, err := repo.ListAccounts(ctx, owner)
	if err != nil {
		return uuid.Nil, err
	}
	for _, acc := range existing {
		if acc.Provider == provider && strings.EqualFold(acc.Address, address) {
			return acc.ID, nil
		}
	}
	return store.NewID(), nil
}

// Token returns the refresh token of an active account.
func (a *Accounts) Token(ctx context.Context, owner, id uuid.UUID) (string, error) {
	acc, err := store.New(a.pool).GetAccount(ctx, owner, id)
	if err != nil {
		return "", err
	}
	if acc.Status != "active" {
		return "", ErrAccountNotUsable
	}
	token, err := a.keys.Open(acc.TokenCiphertext, acc.TokenKeyID, tokenAAD(acc.ID))
	if err != nil {
		return "", fmt.Errorf("open token of account %s: %w", acc.ID, err)
	}
	return string(token), nil
}

// List returns the owner's accounts. The rows carry sealed tokens; callers
// must not pass them on.
func (a *Accounts) List(ctx context.Context, owner uuid.UUID) ([]db.Account, error) {
	return store.New(a.pool).ListAccounts(ctx, owner)
}
