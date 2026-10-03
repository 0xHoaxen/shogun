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

// CompanyNames returns the names of the owner's companies with the given ids,
// by id. Ids that match nothing are left out.
func (r *Repo) CompanyNames(ctx context.Context, owner uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]string, error) {
	names := make(map[uuid.UUID]string, len(ids))
	if len(ids) == 0 {
		return names, nil
	}
	rows, err := r.q.ListCompanyNames(ctx, db.ListCompanyNamesParams{OwnerID: owner, Ids: ids})
	if err != nil {
		return nil, fmt.Errorf("list company names: %w", mapErr(err))
	}
	for _, row := range rows {
		names[row.ID] = row.Name
	}
	return names, nil
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
