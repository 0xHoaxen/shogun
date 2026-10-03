package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/pkg/outbox"
	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store/db"
	"github.com/0xHoaxen/shogun/services/kagami/internal/wire"
)

// Event types and contact_events kinds written by the contact use cases.
const (
	eventContactAdded         = "contact.added"
	eventContactStatusChanged = "contact.status_changed"

	contactEventCreated = "created"
	contactEventStatus  = "status_changed"
)

// DuplicateContactError means the person is already in the contact book, found
// by email and then by LinkedIn URL. Transport maps it to AlreadyExists with
// reason CONTACT_DUPLICATE.
type DuplicateContactError struct {
	ExistingID uuid.UUID
}

func (e *DuplicateContactError) Error() string {
	return fmt.Sprintf("contact already exists: %s", e.ExistingID)
}

// AddContactInput is the input of AddContact.
type AddContactInput struct {
	ContactFields
	// Status defaults to not_reached.
	Status domain.ContactStatus
	// CompanyName creates or finds the company by name when CompanyID is empty.
	CompanyName    string
	IdempotencyKey string
}

// ContactDetail is a contact with its timeline, newest event first.
type ContactDetail struct {
	Contact  db.Contact
	Timeline []db.ContactEvent
}

// ListContactsInput is the input of ListContacts. Zero values do not filter.
type ListContactsInput struct {
	Status    domain.ContactStatus
	Tag       string
	CompanyID string
	Query     string
	PageSize  int32
	PageToken string
}

// ListContactsResult is one page of contacts.
type ListContactsResult struct {
	Contacts      []db.Contact
	NextPageToken string
}

// UpdateContactInput is the input of UpdateContact. Paths are the proto field
// names of the update mask; only those fields of Fields are applied.
type UpdateContactInput struct {
	ID      string
	Version int32
	Paths   []string
	Fields  ContactFields
}

// ChangeContactStatusInput is the input of ChangeContactStatus.
type ChangeContactStatusInput struct {
	ID      string
	To      domain.ContactStatus
	Version int32
}

// AddContact creates a contact. A repeated idempotency key returns the
// original contact and writes nothing; a person already in the book is a
// *DuplicateContactError.
func (s *Service) AddContact(ctx context.Context, in AddContactInput) (db.Contact, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return db.Contact{}, err
	}
	if in.Status == "" {
		in.Status = domain.ContactNotReached
	}
	if !in.Status.Valid() {
		return db.Contact{}, invalid("INVALID_STATUS", "unknown status %q", in.Status)
	}
	if len(in.IdempotencyKey) > maxIdempotencyKeyLen {
		return db.Contact{}, invalid("INVALID_IDEMPOTENCY_KEY", "idempotency key is longer than %d bytes", maxIdempotencyKeyLen)
	}
	values, err := parseContactFields(in.ContactFields)
	if err != nil {
		return db.Contact{}, err
	}

	var created db.Contact
	err = s.inTx(ctx, func(tx pgx.Tx, repo *store.Repo) error {
		if in.IdempotencyKey != "" {
			replay, err := repo.GetContactByIdempotencyKey(ctx, owner, in.IdempotencyKey)
			if err == nil {
				created = replay
				return nil
			}
			if !errors.Is(err, store.ErrNotFound) {
				return err
			}
		}
		created, err = s.createContact(ctx, tx, repo, owner, newContactRow{
			values: values, status: in.Status, companyName: in.CompanyName, idempotencyKey: in.IdempotencyKey,
		})
		return err
	})
	return created, err
}

// newContactRow is what createContact needs to insert one contact.
type newContactRow struct {
	values         contactValues
	status         domain.ContactStatus
	companyName    string
	idempotencyKey string
}

// createContact checks references and duplicates, inserts the contact and its
// created entry, and writes contact.added, all on tx.
func (s *Service) createContact(ctx context.Context, tx pgx.Tx, repo *store.Repo, owner uuid.UUID, row newContactRow) (db.Contact, error) {
	v := row.values
	companyID, err := s.resolveContactCompany(ctx, repo, owner, v.CompanyID, row.companyName)
	if err != nil {
		return db.Contact{}, err
	}
	if err := checkJobRef(ctx, repo, owner, v.JobID); err != nil {
		return db.Contact{}, err
	}
	existing, err := repo.FindDuplicate(ctx, owner, v.Email, v.LinkedinURL)
	if err == nil {
		return db.Contact{}, &DuplicateContactError{ExistingID: existing.ID}
	}
	if !errors.Is(err, store.ErrNotFound) {
		return db.Contact{}, err
	}

	contact, err := repo.InsertContact(ctx, db.InsertContactParams{
		ID: store.NewID(), OwnerID: owner, FullName: v.FullName, CompanyID: companyID, Role: v.Role,
		Email: v.Email, LinkedinUrl: v.LinkedinURL, XHandle: v.XHandle, Phone: v.Phone,
		Relationship: v.Relationship, HowWeMet: v.HowWeMet, Status: string(row.status),
		PreferredChannel: v.PreferredChannel, LastContacted: v.LastContacted, NextFollowUp: v.NextFollowUp,
		TargetRole: v.TargetRole, JobID: v.JobID, Tags: v.Tags, Notes: v.Notes,
		IdempotencyKey: strPtr(row.idempotencyKey),
	})
	if err != nil {
		return db.Contact{}, err
	}
	if err := s.addContactEvent(ctx, repo, contact.ID, contactEventCreated, nil, &contact.Status, nil); err != nil {
		return db.Contact{}, err
	}
	_, err = outbox.Write(ctx, tx, eventSource, eventContactAdded, contact.ID.String(), &kagamiv1.ContactAdded{
		ContactId: contact.ID.String(), Status: wire.ContactStatusToProto(row.status),
	})
	if err != nil {
		return db.Contact{}, fmt.Errorf("write %s event: %w", eventContactAdded, err)
	}
	return contact, nil
}

// resolveContactCompany returns the company id for a contact: the given id
// when it is the owner's, else the company called name, created when missing.
func (s *Service) resolveContactCompany(ctx context.Context, repo *store.Repo, owner uuid.UUID, id *uuid.UUID, name string) (*uuid.UUID, error) {
	if id != nil {
		if _, err := repo.GetCompany(ctx, owner, *id); err != nil {
			return nil, err
		}
		return id, nil
	}
	if name == "" {
		return nil, nil
	}
	company, err := repo.FindOrCreateCompanyByName(ctx, owner, name)
	if err != nil {
		return nil, err
	}
	return &company.ID, nil
}

// checkJobRef fails with ErrNotFound unless id is empty or one of the owner's jobs.
func checkJobRef(ctx context.Context, repo *store.Repo, owner uuid.UUID, id *uuid.UUID) error {
	if id == nil {
		return nil
	}
	_, err := repo.GetJob(ctx, owner, *id)
	return err
}

// addContactEvent appends one entry to a contact's timeline.
func (s *Service) addContactEvent(ctx context.Context, repo *store.Repo, contactID uuid.UUID, kind string, channel, from, to *string) error {
	_, err := repo.InsertContactEvent(ctx, db.InsertContactEventParams{
		ID: store.NewID(), ContactID: contactID, Kind: kind, Channel: channel, FromStatus: from, ToStatus: to,
		Payload: []byte(`{}`), OccurredAt: s.now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("add %s contact event: %w", kind, err)
	}
	return nil
}

// GetContact returns a contact with its timeline.
func (s *Service) GetContact(ctx context.Context, id string) (ContactDetail, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return ContactDetail{}, err
	}
	contactID, err := parseID("id", id)
	if err != nil {
		return ContactDetail{}, err
	}
	repo := store.New(s.pool)
	contact, err := repo.GetContact(ctx, owner, contactID)
	if err != nil {
		return ContactDetail{}, err
	}
	timeline, err := repo.ListContactEvents(ctx, contact.ID)
	if err != nil {
		return ContactDetail{}, err
	}
	return ContactDetail{Contact: contact, Timeline: timeline}, nil
}

// ListContacts returns one page of contacts, most recently updated first.
func (s *Service) ListContacts(ctx context.Context, in ListContactsInput) (ListContactsResult, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return ListContactsResult{}, err
	}
	var f store.ContactFilter
	if in.Status != "" {
		if !in.Status.Valid() {
			return ListContactsResult{}, invalid("INVALID_STATUS", "unknown status %q", in.Status)
		}
		status := string(in.Status)
		f.Status = &status
	}
	f.Tag = trimmedPtr(in.Tag)
	f.Query = trimmedPtr(in.Query)
	if f.CompanyID, err = optionalID("company_id", in.CompanyID); err != nil {
		return ListContactsResult{}, err
	}
	contacts, next, err := store.New(s.pool).ListContacts(ctx, owner, f, store.Page{Size: in.PageSize, Token: in.PageToken})
	if err != nil {
		return ListContactsResult{}, err
	}
	return ListContactsResult{Contacts: contacts, NextPageToken: next}, nil
}

// UpdateContact applies the masked fields when the version is current. It
// writes no outbox event: no consumer needs one for plain edits.
func (s *Service) UpdateContact(ctx context.Context, in UpdateContactInput) (db.Contact, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return db.Contact{}, err
	}
	id, err := parseID("id", in.ID)
	if err != nil {
		return db.Contact{}, err
	}
	if in.Version < 1 {
		return db.Contact{}, invalid("VERSION_REQUIRED", "version is required")
	}

	var updated db.Contact
	err = s.inTx(ctx, func(_ pgx.Tx, repo *store.Repo) error {
		current, err := repo.GetContact(ctx, owner, id)
		if err != nil {
			return err
		}
		if current.Version != in.Version {
			return store.ErrVersionConflict
		}
		merged, err := overlayContactFields(toContactFields(current), in.Paths, in.Fields)
		if err != nil {
			return err
		}
		v, err := parseContactFields(merged)
		if err != nil {
			return err
		}
		if v.CompanyID != nil {
			if _, err := repo.GetCompany(ctx, owner, *v.CompanyID); err != nil {
				return err
			}
		}
		if err := checkJobRef(ctx, repo, owner, v.JobID); err != nil {
			return err
		}
		updated, err = repo.UpdateContact(ctx, db.UpdateContactParams{
			ID: id, OwnerID: owner, Version: in.Version, FullName: v.FullName, CompanyID: v.CompanyID,
			Role: v.Role, Email: v.Email, LinkedinUrl: v.LinkedinURL, XHandle: v.XHandle, Phone: v.Phone,
			Relationship: v.Relationship, HowWeMet: v.HowWeMet, PreferredChannel: v.PreferredChannel,
			LastContacted: v.LastContacted, NextFollowUp: v.NextFollowUp, TargetRole: v.TargetRole,
			JobID: v.JobID, Tags: v.Tags, Notes: v.Notes,
		})
		return err
	})
	return updated, err
}

// ChangeContactStatus moves a contact along the contact state machine. It
// writes a status_changed timeline entry and the contact.status_changed event
// with the update.
func (s *Service) ChangeContactStatus(ctx context.Context, in ChangeContactStatusInput) (db.Contact, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return db.Contact{}, err
	}
	id, err := parseID("id", in.ID)
	if err != nil {
		return db.Contact{}, err
	}
	if in.Version < 1 {
		return db.Contact{}, invalid("VERSION_REQUIRED", "version is required")
	}
	if !in.To.Valid() {
		return db.Contact{}, invalid("INVALID_STATUS", "unknown status %q", in.To)
	}

	var updated db.Contact
	err = s.inTx(ctx, func(tx pgx.Tx, repo *store.Repo) error {
		current, err := repo.GetContact(ctx, owner, id)
		if err != nil {
			return err
		}
		if current.Version != in.Version {
			return store.ErrVersionConflict
		}
		from := domain.ContactStatus(current.Status)
		moved, err := domain.Contact{Status: from}.ChangeStatus(in.To, s.now().UTC())
		if err != nil {
			return err
		}
		updated, err = repo.UpdateContactStatus(ctx, db.UpdateContactStatusParams{
			ID: id, OwnerID: owner, Version: in.Version, Status: string(moved.Status),
			LastContacted: current.LastContacted,
		})
		if err != nil {
			return err
		}

		fromText, toText := string(from), string(moved.Status)
		if err := s.addContactEvent(ctx, repo, id, contactEventStatus, nil, &fromText, &toText); err != nil {
			return err
		}
		_, err = outbox.Write(ctx, tx, eventSource, eventContactStatusChanged, id.String(), &kagamiv1.ContactStatusChanged{
			ContactId: id.String(),
			From:      wire.ContactStatusToProto(from),
			To:        wire.ContactStatusToProto(moved.Status),
		})
		if err != nil {
			return fmt.Errorf("write %s event: %w", eventContactStatusChanged, err)
		}
		return nil
	})
	return updated, err
}
