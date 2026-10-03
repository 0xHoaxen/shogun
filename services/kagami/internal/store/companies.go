package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/kagami/internal/store/db"
)

// GetCompany returns one company of the owner.
func (r *Repo) GetCompany(ctx context.Context, owner, id uuid.UUID) (db.Company, error) {
	c, err := r.q.GetCompany(ctx, db.GetCompanyParams{ID: id, OwnerID: owner})
	return c, mapErr(err)
}

// UpsertCompanyByDomain returns the owner's company for domain, creating it
// with name when there is none. The domain match ignores case.
func (r *Repo) UpsertCompanyByDomain(ctx context.Context, owner uuid.UUID, name, domain string) (db.Company, error) {
	c, err := r.q.UpsertCompanyByDomain(ctx, db.UpsertCompanyByDomainParams{
		ID: NewID(), OwnerID: owner, Name: name, Domain: &domain,
	})
	if err != nil {
		return db.Company{}, fmt.Errorf("upsert company by domain: %w", mapErr(err))
	}
	return c, nil
}

// FindOrCreateCompanyByName returns the owner's company called name, matched
// ignoring case, creating it when there is none.
func (r *Repo) FindOrCreateCompanyByName(ctx context.Context, owner uuid.UUID, name string) (db.Company, error) {
	c, err := r.q.FindCompanyByName(ctx, db.FindCompanyByNameParams{OwnerID: owner, Name: name})
	if err == nil {
		return c, nil
	}
	if found := mapErr(err); !errors.Is(found, ErrNotFound) {
		return db.Company{}, fmt.Errorf("find company by name: %w", found)
	}
	c, err = r.q.InsertCompany(ctx, db.InsertCompanyParams{ID: NewID(), OwnerID: owner, Name: name})
	if err != nil {
		return db.Company{}, fmt.Errorf("insert company: %w", mapErr(err))
	}
	return c, nil
}
