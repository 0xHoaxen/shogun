package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/kagami/internal/store/db"
)

// ContactFilter narrows ListContacts. A nil field does not filter.
type ContactFilter struct {
	Status    *string
	Tag       *string
	CompanyID *uuid.UUID
	// Query matches the name, email and company name, ignoring case.
	Query *string
}

// InsertContact creates a contact. A repeated email, linkedin_url or
// idempotency key is ErrDuplicate.
func (r *Repo) InsertContact(ctx context.Context, arg db.InsertContactParams) (db.Contact, error) {
	c, err := r.q.InsertContact(ctx, arg)
	if err != nil {
		return db.Contact{}, fmt.Errorf("insert contact: %w", mapErr(err))
	}
	return c, nil
}

// GetContact returns one contact of the owner.
func (r *Repo) GetContact(ctx context.Context, owner, id uuid.UUID) (db.Contact, error) {
	c, err := r.q.GetContact(ctx, db.GetContactParams{ID: id, OwnerID: owner})
	return c, mapErr(err)
}

// GetContactByIdempotencyKey returns the contact a retried AddContact created
// earlier.
func (r *Repo) GetContactByIdempotencyKey(ctx context.Context, owner uuid.UUID, key string) (db.Contact, error) {
	c, err := r.q.GetContactByIdempotencyKey(ctx, db.GetContactByIdempotencyKeyParams{OwnerID: owner, IdempotencyKey: key})
	return c, mapErr(err)
}

// FindDuplicate returns the owner's existing contact for the same person:
// first by email, ignoring case, then by linkedin_url. Empty or nil values are
// skipped. It returns ErrNotFound when nobody matches.
func (r *Repo) FindDuplicate(ctx context.Context, owner uuid.UUID, email, linkedinURL *string) (db.Contact, error) {
	if email != nil && *email != "" {
		c, err := r.q.FindContactByEmail(ctx, db.FindContactByEmailParams{OwnerID: owner, Email: *email})
		if err == nil {
			return c, nil
		}
		if found := mapErr(err); !errors.Is(found, ErrNotFound) {
			return db.Contact{}, fmt.Errorf("find contact by email: %w", found)
		}
	}
	if linkedinURL != nil && *linkedinURL != "" {
		c, err := r.q.FindContactByLinkedin(ctx, db.FindContactByLinkedinParams{OwnerID: owner, LinkedinUrl: *linkedinURL})
		if err != nil {
			return db.Contact{}, fmt.Errorf("find contact by linkedin: %w", mapErr(err))
		}
		return c, nil
	}
	return db.Contact{}, ErrNotFound
}

// ListContacts returns one page of the owner's contacts, most recently updated
// first, and the token for the next page, empty on the last page.
func (r *Repo) ListContacts(ctx context.Context, owner uuid.UUID, f ContactFilter, p Page) ([]db.Contact, string, error) {
	after, err := decodeCursor(p.Token)
	if err != nil {
		return nil, "", err
	}
	size := p.size()
	afterAt, afterID := afterParams(after)
	rows, err := r.q.ListContacts(ctx, db.ListContactsParams{
		OwnerID: owner, Status: f.Status, Tag: f.Tag, CompanyID: f.CompanyID, Query: f.Query,
		AfterUpdatedAt: afterAt, AfterID: afterID, RowLimit: size + 1,
	})
	if err != nil {
		return nil, "", fmt.Errorf("list contacts: %w", mapErr(err))
	}
	rows, next := trimPage(rows, size, func(c db.Contact) (time.Time, uuid.UUID) { return c.UpdatedAt, c.ID })
	return rows, next, nil
}

// UpdateContact writes the editable columns when arg.Version is current. A
// stale version is ErrVersionConflict.
func (r *Repo) UpdateContact(ctx context.Context, arg db.UpdateContactParams) (db.Contact, error) {
	c, err := r.q.UpdateContact(ctx, arg)
	if err != nil {
		return db.Contact{}, staleOrMissing(err, func() error {
			_, getErr := r.q.GetContact(ctx, db.GetContactParams{ID: arg.ID, OwnerID: arg.OwnerID})
			return getErr
		})
	}
	return c, nil
}

// UpdateContactStatus writes the status when arg.Version is current. A stale
// version is ErrVersionConflict.
func (r *Repo) UpdateContactStatus(ctx context.Context, arg db.UpdateContactStatusParams) (db.Contact, error) {
	c, err := r.q.UpdateContactStatus(ctx, arg)
	if err != nil {
		return db.Contact{}, staleOrMissing(err, func() error {
			_, getErr := r.q.GetContact(ctx, db.GetContactParams{ID: arg.ID, OwnerID: arg.OwnerID})
			return getErr
		})
	}
	return c, nil
}

// InsertContactEvent appends to a contact's timeline.
func (r *Repo) InsertContactEvent(ctx context.Context, arg db.InsertContactEventParams) (db.ContactEvent, error) {
	e, err := r.q.InsertContactEvent(ctx, arg)
	if err != nil {
		return db.ContactEvent{}, fmt.Errorf("insert contact event: %w", mapErr(err))
	}
	return e, nil
}

// ListContactEvents returns a contact's timeline, newest first.
func (r *Repo) ListContactEvents(ctx context.Context, contactID uuid.UUID) ([]db.ContactEvent, error) {
	events, err := r.q.ListContactEvents(ctx, contactID)
	if err != nil {
		return nil, fmt.Errorf("list contact events: %w", mapErr(err))
	}
	return events, nil
}

// InsertImport records one CSV import run.
func (r *Repo) InsertImport(ctx context.Context, arg db.InsertImportParams) (db.Import, error) {
	i, err := r.q.InsertImport(ctx, arg)
	if err != nil {
		return db.Import{}, fmt.Errorf("insert import: %w", mapErr(err))
	}
	return i, nil
}
