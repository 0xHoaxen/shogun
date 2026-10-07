// Package app holds the sensei use cases.
package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/services/sensei/internal/domain"
	"github.com/0xHoaxen/shogun/services/sensei/internal/store"
)

// Service runs the analytics use cases.
type Service struct{}

// NewService returns a Service.
func NewService() *Service { return &Service{} }

// RecordFact stores the fact an event caused, in tx, the transaction that also
// marks the event as seen. A fact that can never be stored is
// domain.ErrInvalidFact. A repeated event stores nothing.
func (s *Service) RecordFact(ctx context.Context, tx pgx.Tx, f domain.Fact) error {
	clean, err := f.Validate()
	if err != nil {
		return fmt.Errorf("record fact: %w", err)
	}
	if _, err := store.New(tx).InsertFact(ctx, clean); err != nil {
		return fmt.Errorf("record fact: %w", err)
	}
	return nil
}

// IsInvalid reports whether err is a fact that no retry can fix.
func IsInvalid(err error) bool { return errors.Is(err, domain.ErrInvalidFact) }
