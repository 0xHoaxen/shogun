package app

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/soroban/internal/store"
	"github.com/0xHoaxen/shogun/services/soroban/internal/store/db"
)

// PriceInput is a model's rates from a date, in micro-dollars per million
// tokens.
type PriceInput struct {
	Model                   string
	EffectiveFrom           string // YYYY-MM-DD
	InputMicrosPerMtok      int64
	OutputMicrosPerMtok     int64
	CacheReadMicrosPerMtok  int64
	CacheWriteMicrosPerMtok int64
}

// SetPrice stores a price, replacing one for the same model and date. Older
// ledger rows keep the price that applied when they were written.
func (s *Service) SetPrice(ctx context.Context, in PriceInput) (db.Price, error) {
	if _, err := ownerFrom(ctx); err != nil {
		return db.Price{}, err
	}
	from, err := time.Parse(time.DateOnly, in.EffectiveFrom)
	if err != nil {
		return db.Price{}, invalid(ReasonInvalidPrice, "effective_from %q is not YYYY-MM-DD", in.EffectiveFrom)
	}
	if in.Model == "" || in.InputMicrosPerMtok < 0 || in.OutputMicrosPerMtok < 0 ||
		in.CacheReadMicrosPerMtok < 0 || in.CacheWriteMicrosPerMtok < 0 {
		return db.Price{}, invalid(ReasonInvalidPrice, "a model and non-negative rates are required")
	}
	var price db.Price
	err = s.inTx(ctx, func(_ pgx.Tx, repo *store.Repo) error {
		var err error
		price, err = repo.UpsertPrice(ctx, db.UpsertPriceParams{
			Model: in.Model, EffectiveFrom: from,
			InputMicrosPerMtok: in.InputMicrosPerMtok, OutputMicrosPerMtok: in.OutputMicrosPerMtok,
			CacheReadMicrosPerMtok: in.CacheReadMicrosPerMtok, CacheWriteMicrosPerMtok: in.CacheWriteMicrosPerMtok,
		})
		return err
	})
	return price, err
}

// ListPrices returns every price.
func (s *Service) ListPrices(ctx context.Context) ([]db.Price, error) {
	if _, err := ownerFrom(ctx); err != nil {
		return nil, err
	}
	return store.New(s.pool).ListPrices(ctx)
}
