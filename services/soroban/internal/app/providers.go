package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/soroban/internal/domain"
	"github.com/0xHoaxen/shogun/services/soroban/internal/store"
	"github.com/0xHoaxen/shogun/services/soroban/internal/store/db"
)

// providerAADPrefix binds a sealed key to the owner it belongs to, so a row
// copied to another user's id will not open.
const providerAADPrefix = "soroban.providers.api_key/"

// ProviderInput is what a user sets: their AI provider.
type ProviderInput struct {
	BaseURL string
	Model   string
	// APIKey is required to create a provider. When updating, empty keeps the
	// stored key.
	APIKey              string
	InputMicrosPerMTok  *int64
	OutputMicrosPerMTok *int64
	// Version is 0 to create, otherwise the version being updated.
	Version int32
}

// ProviderView is a stored provider without its key.
type ProviderView struct {
	BaseURL             string
	Model               string
	KeyLast4            string
	InputMicrosPerMTok  *int64
	OutputMicrosPerMTok *int64
	Version             int32
	UpdatedAt           time.Time
}

// ProviderSecret is a provider with its key opened, for the call it is about to
// make. It never leaves soroban except to an authenticated internal caller.
type ProviderSecret struct {
	BaseURL             string
	Model               string
	APIKey              string
	InputMicrosPerMTok  *int64
	OutputMicrosPerMTok *int64
}

func viewOf(row db.Provider) ProviderView {
	return ProviderView{
		BaseURL: row.BaseUrl, Model: row.Model, KeyLast4: row.KeyLast4,
		InputMicrosPerMTok: row.InputMicrosPerMtok, OutputMicrosPerMTok: row.OutputMicrosPerMtok,
		Version: row.Version, UpdatedAt: row.UpdatedAt,
	}
}

func providerAAD(owner uuid.UUID) []byte { return []byte(providerAADPrefix + owner.String()) }

// SetProvider stores the caller's provider. The key is sealed before it is
// written and is never returned or logged.
func (s *Service) SetProvider(ctx context.Context, in ProviderInput) (ProviderView, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return ProviderView{}, err
	}
	if s.keys == nil {
		return ProviderView{}, ErrProviderStorageOff
	}
	fields, err := s.checkProvider(in)
	if err != nil {
		return ProviderView{}, err
	}

	var view ProviderView
	err = s.inTx(ctx, func(_ pgx.Tx, repo *store.Repo) error {
		if in.Version == 0 {
			return s.createProvider(ctx, repo, owner, in, fields, &view)
		}
		return s.updateProvider(ctx, repo, owner, in, fields, &view)
	})
	if err != nil {
		return ProviderView{}, err
	}
	return view, nil
}

// checkProvider validates what the user typed and returns the address in its
// normal form. Errors name the field and never repeat the input.
func (s *Service) checkProvider(in ProviderInput) (normalized string, err error) {
	baseURL, err := domain.NormalizeBaseURL(in.BaseURL, s.insecureProviders)
	if err != nil {
		return "", invalid(ReasonInvalidProviderURL, "the provider address must be a public https address")
	}
	if err := domain.ValidateModel(in.Model); err != nil {
		return "", invalid(ReasonInvalidProviderModel, "the model name is not valid")
	}
	if in.APIKey != "" {
		if err := domain.ValidateAPIKey(in.APIKey); err != nil {
			return "", invalid(ReasonInvalidAPIKey, "the API key is not valid")
		}
	}
	if err := domain.ValidateProviderPrices(in.InputMicrosPerMTok, in.OutputMicrosPerMTok); err != nil {
		return "", invalid(ReasonInvalidPrice, "set both prices, each between 0 and 1000000000000")
	}
	return baseURL, nil
}

func (s *Service) createProvider(ctx context.Context, repo *store.Repo, owner uuid.UUID, in ProviderInput, baseURL string, out *ProviderView) error {
	if in.APIKey == "" {
		return invalid(ReasonAPIKeyRequired, "an API key is required")
	}
	sealed, keyID, err := s.keys.Seal([]byte(in.APIKey), providerAAD(owner))
	if err != nil {
		return fmt.Errorf("seal api key: %w", err)
	}
	row, err := repo.InsertProvider(ctx, db.InsertProviderParams{
		OwnerID: owner, BaseUrl: baseURL, Model: in.Model, ApiKeyCiphertext: sealed, KeyID: keyID,
		KeyLast4: domain.KeyLast4(in.APIKey), InputMicrosPerMtok: in.InputMicrosPerMTok, OutputMicrosPerMtok: in.OutputMicrosPerMTok,
	})
	if errors.Is(err, store.ErrDuplicate) {
		return store.ErrVersionConflict // one already exists: the caller read an older state
	}
	if err != nil {
		return err
	}
	*out = viewOf(row)
	return nil
}

func (s *Service) updateProvider(ctx context.Context, repo *store.Repo, owner uuid.UUID, in ProviderInput, baseURL string, out *ProviderView) error {
	current, err := repo.GetProvider(ctx, owner)
	if errors.Is(err, store.ErrNotFound) {
		return store.ErrVersionConflict
	}
	if err != nil {
		return err
	}
	ciphertext, keyID, last4 := current.ApiKeyCiphertext, current.KeyID, current.KeyLast4
	if in.APIKey != "" {
		if ciphertext, keyID, err = s.keys.Seal([]byte(in.APIKey), providerAAD(owner)); err != nil {
			return fmt.Errorf("seal api key: %w", err)
		}
		last4 = domain.KeyLast4(in.APIKey)
	}
	row, err := repo.UpdateProvider(ctx, db.UpdateProviderParams{
		OwnerID: owner, Version: in.Version, BaseUrl: baseURL, Model: in.Model, ApiKeyCiphertext: ciphertext,
		KeyID: keyID, KeyLast4: last4, InputMicrosPerMtok: in.InputMicrosPerMTok, OutputMicrosPerMtok: in.OutputMicrosPerMTok,
	})
	if err != nil {
		return err
	}
	*out = viewOf(row)
	return nil
}

// GetProvider returns the caller's provider without its key, or
// store.ErrNotFound when there is none.
func (s *Service) GetProvider(ctx context.Context) (ProviderView, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return ProviderView{}, err
	}
	row, err := store.New(s.pool).GetProvider(ctx, owner)
	if err != nil {
		return ProviderView{}, err
	}
	return viewOf(row), nil
}

// DeleteProvider removes the caller's provider and key. Having none is not an
// error.
func (s *Service) DeleteProvider(ctx context.Context) error {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return err
	}
	_, err = store.New(s.pool).DeleteProvider(ctx, owner)
	return err
}

// ProviderSecret opens the caller's provider with its key. It is
// ErrProviderNotConfigured when the caller has none, and an error when the
// stored key cannot be opened, which includes a row copied from another owner.
func (s *Service) ProviderSecret(ctx context.Context) (ProviderSecret, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return ProviderSecret{}, err
	}
	return s.providerSecretFor(ctx, store.New(s.pool), owner)
}

func (s *Service) providerSecretFor(ctx context.Context, repo *store.Repo, owner uuid.UUID) (ProviderSecret, error) {
	if s.keys == nil {
		return ProviderSecret{}, ErrProviderStorageOff
	}
	row, err := repo.GetProvider(ctx, owner)
	if errors.Is(err, store.ErrNotFound) {
		return ProviderSecret{}, ErrProviderNotConfigured
	}
	if err != nil {
		return ProviderSecret{}, err
	}
	key, err := s.keys.Open(row.ApiKeyCiphertext, row.KeyID, providerAAD(owner))
	if err != nil {
		return ProviderSecret{}, fmt.Errorf("open api key: %w", err)
	}
	return ProviderSecret{
		BaseURL: row.BaseUrl, Model: row.Model, APIKey: string(key),
		InputMicrosPerMTok: row.InputMicrosPerMtok, OutputMicrosPerMTok: row.OutputMicrosPerMtok,
	}, nil
}
