package store_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/kagami/internal/store"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store/db"
)

func TestJobTimelineListsNewestFirst(t *testing.T) {
	ctx, r := newRepo(t)
	owner := store.NewID()
	job := newJob(ctx, t, r, owner, "Platform Engineer")
	base := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)

	for i, kind := range []string{"created", "status_changed"} {
		if _, err := r.InsertJobEvent(ctx, db.InsertJobEventParams{
			ID: store.NewID(), JobID: job.ID, Kind: kind, Payload: []byte(`{}`),
			OccurredAt: base.Add(time.Duration(i) * 24 * time.Hour),
		}); err != nil {
			t.Fatalf("insert %s: %v", kind, err)
		}
	}

	events, err := r.ListJobEvents(ctx, job.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 2 || events[0].Kind != "status_changed" || events[1].Kind != "created" {
		t.Fatalf("got %+v, want status_changed then created", events)
	}
}

func TestContactTimelineUpdateAndIdempotencyLookup(t *testing.T) {
	ctx, r := newRepo(t)
	owner := store.NewID()
	c, err := r.InsertContact(ctx, db.InsertContactParams{
		ID: store.NewID(), OwnerID: owner, FullName: "Kenji Watanabe", Status: "not_reached",
		Tags: []string{"northwind"}, IdempotencyKey: ptr("add-kenji"),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	byKey, err := r.GetContactByIdempotencyKey(ctx, owner, "add-kenji")
	if err != nil || byKey.ID != c.ID {
		t.Fatalf("lookup by key: %+v, %v", byKey, err)
	}
	if _, err := r.GetContactByIdempotencyKey(ctx, owner, "other"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown key: got %v, want ErrNotFound", err)
	}

	updated, err := r.UpdateContact(ctx, db.UpdateContactParams{
		ID: c.ID, OwnerID: owner, Version: c.Version, FullName: "Kenji W.",
		Tags: []string{"northwind", "referral"},
	})
	if err != nil || updated.Version != c.Version+1 || updated.FullName != "Kenji W." {
		t.Fatalf("update: %+v, %v", updated, err)
	}
	if _, err := r.UpdateContact(ctx, db.UpdateContactParams{
		ID: c.ID, OwnerID: owner, Version: c.Version, FullName: "Stale", Tags: []string{},
	}); !errors.Is(err, store.ErrVersionConflict) {
		t.Fatalf("stale update: got %v, want ErrVersionConflict", err)
	}
	got, err := r.GetContact(ctx, owner, c.ID)
	if err != nil || got.FullName != "Kenji W." {
		t.Fatalf("get: %+v, %v", got, err)
	}

	if _, err := r.InsertContactEvent(ctx, db.InsertContactEventParams{
		ID: store.NewID(), ContactID: c.ID, Kind: "created", Payload: []byte(`{}`),
		OccurredAt: time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("insert event: %v", err)
	}
	events, err := r.ListContactEvents(ctx, c.ID)
	if err != nil || len(events) != 1 {
		t.Fatalf("timeline: %d events, %v", len(events), err)
	}
}

func TestListContactsFiltersByTagAndPaginates(t *testing.T) {
	ctx, r := newRepo(t)
	owner := store.NewID()
	for _, name := range []string{"A", "B", "C"} {
		if _, err := r.InsertContact(ctx, db.InsertContactParams{
			ID: store.NewID(), OwnerID: owner, FullName: name, Status: "not_reached",
			Tags: []string{"go"},
		}); err != nil {
			t.Fatalf("insert %s: %v", name, err)
		}
	}
	if _, err := newContact(ctx, r, owner, "No tag", nil); err != nil {
		t.Fatalf("insert: %v", err)
	}

	first, next, err := r.ListContacts(ctx, owner, store.ContactFilter{Tag: ptr("go")}, store.Page{Size: 2})
	if err != nil || len(first) != 2 || next == "" {
		t.Fatalf("first page: %d rows, token %q, %v", len(first), next, err)
	}
	second, next, err := r.ListContacts(ctx, owner, store.ContactFilter{Tag: ptr("go")}, store.Page{Size: 2, Token: next})
	if err != nil || len(second) != 1 || next != "" {
		t.Fatalf("second page: %d rows, token %q, %v", len(second), next, err)
	}
	seen := map[uuid.UUID]bool{}
	for _, c := range append(first, second...) {
		seen[c.ID] = true
	}
	if len(seen) != 3 {
		t.Fatalf("got %d distinct contacts, want 3", len(seen))
	}
}

func TestInsertImportRecordsCounts(t *testing.T) {
	ctx, r := newRepo(t)
	imp, err := r.InsertImport(ctx, db.InsertImportParams{
		ID: store.NewID(), OwnerID: store.NewID(), Filename: "contacts-sept.csv",
		RowsTotal: 18, RowsCreated: 15, RowsUpdated: 2, RowsFailed: 1,
		Errors: []byte(`[{"row":4,"column":"full_name","message":"required"}]`),
	})
	if err != nil {
		t.Fatalf("insert import: %v", err)
	}
	if imp.RowsTotal != 18 || imp.RowsFailed != 1 {
		t.Fatalf("got %+v", imp)
	}
}

func TestCompanyLookups(t *testing.T) {
	ctx, r := newRepo(t)
	owner := store.NewID()

	first, err := r.FindOrCreateCompanyByName(ctx, owner, "Quayside")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	again, err := r.FindOrCreateCompanyByName(ctx, owner, "QUAYSIDE")
	if err != nil || again.ID != first.ID {
		t.Fatalf("find ignoring case: %+v, %v", again, err)
	}
	if _, err := r.GetCompany(ctx, owner, first.ID); err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, err := r.GetCompany(ctx, store.NewID(), first.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("other owner: got %v, want ErrNotFound", err)
	}
}
