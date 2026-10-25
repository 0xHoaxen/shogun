package app

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/pkg/outbox"
	"github.com/0xHoaxen/shogun/pkg/owners"
	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store/db"
)

const (
	eventContactNoteAdded = "contact.note_added"
	contactEventNote      = "note"

	// MaxNoteLength is the longest note, in characters.
	MaxNoteLength = 2000
	// LinkedJobsLimit is how many jobs at the contact's company GetContact shows.
	LinkedJobsLimit = 20

	reasonInvalidNote = "INVALID_NOTE"
	reasonNoteTooLong = "NOTE_TOO_LONG"
)

// AddContactNote adds a note to a contact's timeline and writes the
// contact.note_added event with it. A note does not change the contact, so the
// contact's version stays as it is and a note never conflicts with an edit.
func (s *Service) AddContactNote(ctx context.Context, id, text string) (db.ContactEvent, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return db.ContactEvent{}, err
	}
	contactID, err := parseID("id", id)
	if err != nil {
		return db.ContactEvent{}, err
	}
	text = strings.TrimSpace(text)
	switch {
	case text == "":
		return db.ContactEvent{}, invalidField("text", reasonInvalidNote, "write something to add a note")
	case utf8.RuneCountInString(text) > MaxNoteLength:
		return db.ContactEvent{}, invalidField("text", reasonNoteTooLong, "a note is at most %d characters", MaxNoteLength)
	}

	var note db.ContactEvent
	err = s.inTx(ctx, func(tx pgx.Tx, repo *store.Repo) error {
		contact, err := repo.GetContact(ctx, owner, contactID)
		if err != nil {
			return err
		}
		note, err = repo.InsertContactEvent(ctx, db.InsertContactEventParams{
			ID: store.NewID(), ContactID: contact.ID, Kind: contactEventNote,
			Payload: jsonObject(map[string]any{"text": text}), OccurredAt: s.now().UTC(),
		})
		if err != nil {
			return fmt.Errorf("add note: %w", err)
		}
		_, err = outbox.Write(ctx, tx, eventSource, eventContactNoteAdded, contact.ID.String(), &kagamiv1.ContactNoteAdded{
			ContactId: contact.ID.String(), NoteId: note.ID.String(), OwnerId: owner.String(),
		})
		if err != nil {
			return fmt.Errorf("write %s event: %w", eventContactNoteAdded, err)
		}
		return nil
	})
	return note, err
}

// MarkContactedInput is the input of MarkContacted.
type MarkContactedInput struct {
	ID      string
	Version int32
	// On is YYYY-MM-DD; empty means today in the owner's time zone.
	On string
}

// MarkContacted records that the owner reached the contact themselves. A contact
// not yet reached moves to reached_out (a status change like any other, with its
// event); for any other status only last_contacted changes. A status never moves
// backwards.
func (s *Service) MarkContacted(ctx context.Context, in MarkContactedInput) (db.Contact, error) {
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
	today, err := s.ownerToday(ctx, owner)
	if err != nil {
		return db.Contact{}, err
	}
	on := today
	if parsed, err := parseDate("on", in.On); err != nil {
		return db.Contact{}, err
	} else if parsed != nil {
		on = *parsed
	}
	if on.After(today) {
		return db.Contact{}, invalidField("on", "INVALID_DATE", "the date cannot be in the future")
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
		if domain.ContactStatus(current.Status) == domain.ContactNotReached {
			updated, err = s.moveContact(ctx, tx, repo, owner, current, domain.ContactReachedOut, &on, map[string]any{"manual": true})
			return err
		}
		updated, err = repo.UpdateContactLastContacted(ctx, owner, current, &on)
		if err != nil {
			return err
		}
		return s.addContactEvent(ctx, repo, current.ID, contactEventMessageSent, nil, nil, nil, map[string]any{"manual": true, "on": on.Format(dateLayout)})
	})
	return updated, err
}

// ownerToday is today's date in the owner's time zone, as a UTC midnight. An
// owner kagami has not heard of yet, or with an unreadable zone, gets UTC.
func (s *Service) ownerToday(ctx context.Context, owner uuid.UUID) (time.Time, error) {
	loc := time.UTC
	o, err := owners.Get(ctx, s.pool, owner)
	switch {
	case err == nil:
		if l, locErr := time.LoadLocation(o.Timezone); locErr == nil {
			loc = l
		}
	case !errors.Is(err, owners.ErrNotFound):
		return time.Time{}, fmt.Errorf("get owner: %w", err)
	}
	y, m, d := s.now().In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC), nil
}

// linkedJobs returns the owner's jobs at a company, newest first.
func (s *Service) linkedJobs(ctx context.Context, repo *store.Repo, owner uuid.UUID, companyID *uuid.UUID) ([]db.Job, error) {
	if companyID == nil {
		return nil, nil
	}
	jobs, _, err := repo.ListJobs(ctx, owner, store.JobFilter{CompanyID: companyID}, store.Page{Size: LinkedJobsLimit})
	return jobs, err
}
