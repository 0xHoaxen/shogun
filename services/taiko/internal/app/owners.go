package app

import (
	"context"

	"github.com/0xHoaxen/shogun/pkg/owners"
)

// ActiveOwners lists the owners torii has told taiko about who may be sent a
// digest.
func (s *Service) ActiveOwners(ctx context.Context) ([]owners.Owner, error) {
	return owners.ListActive(ctx, s.pool)
}
