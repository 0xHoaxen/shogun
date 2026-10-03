package grpc_test

import (
	"slices"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
)

func (h *harness) addContact(t *testing.T, name, email, key string) *kagamiv1.Contact {
	t.Helper()
	res, err := h.client.AddContact(h.ctx(t), &kagamiv1.AddContactRequest{
		Contact:        &kagamiv1.Contact{FullName: name, Email: email, Tags: []string{"Go"}},
		CompanyName:    "Lumen",
		IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("add contact: %v", err)
	}
	return res.GetContact()
}

func TestAddContactWritesOneOutboxEventAndATimelineEntry(t *testing.T) {
	h := newHarness(t)

	c := h.addContact(t, "Priya Raman", "priya@lumen.example", "")

	if c.GetStatus() != kagamiv1.ContactStatus_CONTACT_STATUS_NOT_REACHED || c.GetCompanyId() == "" ||
		!slices.Equal(c.GetTags(), []string{"go"}) {
		t.Fatalf("got %+v", c)
	}
	if c.GetCompanyName() != "Lumen" {
		t.Fatalf("got company name %q, want Lumen", c.GetCompanyName())
	}
	if n := h.outboxCount(t, "contact.added"); n != 1 {
		t.Fatalf("got %d contact.added rows, want 1", n)
	}
	detail, err := h.client.GetContact(h.ctx(t), &kagamiv1.GetContactRequest{Id: c.GetId()})
	if err != nil || len(detail.GetTimeline()) != 1 || detail.GetTimeline()[0].GetKind() != "created" {
		t.Fatalf("timeline: %+v, %v", detail.GetTimeline(), err)
	}
}

func TestAddContactReplaysIdempotencyKey(t *testing.T) {
	h := newHarness(t)

	first := h.addContact(t, "Arjun Mehta", "arjun@corvid.example", "add-arjun")
	again := h.addContact(t, "Arjun Mehta", "arjun@corvid.example", "add-arjun")

	if first.GetId() != again.GetId() || first.GetVersion() != again.GetVersion() {
		t.Fatalf("replay returned %s v%d, want %s v%d", again.GetId(), again.GetVersion(), first.GetId(), first.GetVersion())
	}
	if n := h.outboxCount(t, ""); n != 1 {
		t.Fatalf("got %d outbox rows after a replay, want 1", n)
	}
}

func TestAddContactRefusesAPersonAlreadyInTheBook(t *testing.T) {
	h := newHarness(t)
	h.addContact(t, "Priya Raman", "priya@lumen.example", "")

	_, err := h.client.AddContact(h.ctx(t), &kagamiv1.AddContactRequest{
		Contact: &kagamiv1.Contact{FullName: "P. Raman", Email: "PRIYA@lumen.example"},
	})

	requireStatus(t, err, codes.AlreadyExists, "CONTACT_DUPLICATE")
	if n := h.outboxCount(t, ""); n != 1 {
		t.Fatalf("got %d outbox rows, want only the first contact's", n)
	}
}

func TestAddContactRejectsBadInput(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		name   string
		code   codes.Code
		req    *kagamiv1.AddContactRequest
		reason string
	}{
		{"no name", codes.InvalidArgument, &kagamiv1.AddContactRequest{}, "FULL_NAME_REQUIRED"},
		{"bad email", codes.InvalidArgument, &kagamiv1.AddContactRequest{Contact: &kagamiv1.Contact{FullName: "A", Email: "nope"}}, "INVALID_EMAIL"},
		{"unknown status", codes.InvalidArgument, &kagamiv1.AddContactRequest{Contact: &kagamiv1.Contact{FullName: "A", Status: kagamiv1.ContactStatus(42)}}, "INVALID_STATUS"},
		{"other owner's company", codes.NotFound, &kagamiv1.AddContactRequest{Contact: &kagamiv1.Contact{FullName: "A", CompanyId: "0198f000-0000-7000-8000-000000000001"}}, "RESOURCE_NOT_FOUND"},
		{"unknown job", codes.NotFound, &kagamiv1.AddContactRequest{Contact: &kagamiv1.Contact{FullName: "A", JobId: "0198f000-0000-7000-8000-000000000002"}}, "RESOURCE_NOT_FOUND"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.AddContact(h.ctx(t), tt.req)
			requireStatus(t, err, tt.code, tt.reason)
		})
	}
	if n := h.outboxCount(t, ""); n != 0 {
		t.Fatalf("got %d outbox rows after failed calls, want 0", n)
	}
}

func TestChangeContactStatusFollowsTheStateMachine(t *testing.T) {
	h := newHarness(t)
	c := h.addContact(t, "Mira Okafor", "mira@corvid.example", "")

	moved, err := h.client.ChangeContactStatus(h.ctx(t), &kagamiv1.ChangeContactStatusRequest{
		Id: c.GetId(), ToStatus: kagamiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT, Version: c.GetVersion(),
	})
	if err != nil {
		t.Fatalf("not_reached to reached_out: %v", err)
	}
	if moved.GetContact().GetStatus() != kagamiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT || moved.GetContact().GetVersion() != 2 {
		t.Fatalf("got %+v", moved.GetContact())
	}
	if n := h.outboxCount(t, "contact.status_changed"); n != 1 {
		t.Fatalf("got %d contact.status_changed rows, want 1", n)
	}

	// reached_out cannot jump straight to referral_asked.
	_, err = h.client.ChangeContactStatus(h.ctx(t), &kagamiv1.ChangeContactStatusRequest{
		Id: c.GetId(), ToStatus: kagamiv1.ContactStatus_CONTACT_STATUS_REFERRAL_ASKED, Version: 2,
	})
	requireStatus(t, err, codes.FailedPrecondition, "CONTACT_STATUS_INVALID_TRANSITION")

	_, err = h.client.ChangeContactStatus(h.ctx(t), &kagamiv1.ChangeContactStatusRequest{
		Id: c.GetId(), ToStatus: kagamiv1.ContactStatus_CONTACT_STATUS_REPLIED, Version: c.GetVersion(),
	})
	requireStatus(t, err, codes.Aborted, "VERSION_CONFLICT")

	if n := h.outboxCount(t, "contact.status_changed"); n != 1 {
		t.Fatalf("failed calls left %d contact.status_changed rows, want 1", n)
	}
	_, err = h.client.ChangeContactStatus(h.ctx(t), &kagamiv1.ChangeContactStatusRequest{Id: c.GetId(), Version: 2})
	requireStatus(t, err, codes.InvalidArgument, "INVALID_STATUS")
}

func TestUpdateContactAppliesTheMaskOnly(t *testing.T) {
	h := newHarness(t)
	c := h.addContact(t, "Kenji Watanabe", "kenji@northwind.example", "")
	before := h.outboxCount(t, "")

	res, err := h.client.UpdateContact(h.ctx(t), &kagamiv1.UpdateContactRequest{
		Contact:    &kagamiv1.Contact{Id: c.GetId(), FullName: "Kenji W.", Notes: "ignored", Tags: []string{"Referral", "go"}},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"full_name", "tags"}},
		Version:    c.GetVersion(),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	got := res.GetContact()
	if got.GetFullName() != "Kenji W." || got.GetNotes() != "" || got.GetEmail() != "kenji@northwind.example" ||
		!slices.Equal(got.GetTags(), []string{"go", "referral"}) || got.GetVersion() != c.GetVersion()+1 {
		t.Fatalf("got %+v", got)
	}
	if n := h.outboxCount(t, ""); n != before {
		t.Fatalf("update wrote %d outbox rows, want none", n-before)
	}

	for _, tt := range []struct {
		name   string
		paths  []string
		reason string
	}{
		{"status", []string{"status"}, "STATUS_NOT_EDITABLE"},
		{"unknown field", []string{"idempotency_key"}, "UNKNOWN_FIELD"},
		{"empty mask", nil, "UPDATE_MASK_REQUIRED"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.UpdateContact(h.ctx(t), &kagamiv1.UpdateContactRequest{
				Contact:    &kagamiv1.Contact{Id: c.GetId()},
				UpdateMask: &fieldmaskpb.FieldMask{Paths: tt.paths},
				Version:    got.GetVersion(),
			})
			requireStatus(t, err, codes.InvalidArgument, tt.reason)
		})
	}

	_, err = h.client.UpdateContact(h.ctx(t), &kagamiv1.UpdateContactRequest{
		Contact:    &kagamiv1.Contact{Id: c.GetId(), FullName: "Stale"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"full_name"}},
		Version:    c.GetVersion(),
	})
	requireStatus(t, err, codes.Aborted, "VERSION_CONFLICT")
}

func TestListContactsFiltersAndPaginates(t *testing.T) {
	h := newHarness(t)
	h.addContact(t, "Ada", "ada@x.example", "")
	h.addContact(t, "Bea", "bea@x.example", "")
	moved := h.addContact(t, "Cy", "cy@x.example", "")
	if _, err := h.client.ChangeContactStatus(h.ctx(t), &kagamiv1.ChangeContactStatusRequest{
		Id: moved.GetId(), ToStatus: kagamiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT, Version: moved.GetVersion(),
	}); err != nil {
		t.Fatalf("change status: %v", err)
	}

	first, err := h.client.ListContacts(h.ctx(t), &kagamiv1.ListContactsRequest{
		Status: kagamiv1.ContactStatus_CONTACT_STATUS_NOT_REACHED, PageSize: 1,
	})
	if err != nil || len(first.GetContacts()) != 1 || first.GetNextPageToken() == "" {
		t.Fatalf("first page: %d contacts, token %q, %v", len(first.GetContacts()), first.GetNextPageToken(), err)
	}
	second, err := h.client.ListContacts(h.ctx(t), &kagamiv1.ListContactsRequest{
		Status: kagamiv1.ContactStatus_CONTACT_STATUS_NOT_REACHED, PageSize: 1, PageToken: first.GetNextPageToken(),
	})
	if err != nil || len(second.GetContacts()) != 1 || second.GetNextPageToken() != "" {
		t.Fatalf("second page: %d contacts, token %q, %v", len(second.GetContacts()), second.GetNextPageToken(), err)
	}
	if first.GetContacts()[0].GetId() == second.GetContacts()[0].GetId() {
		t.Fatal("the two pages repeat a contact")
	}

	byQuery, err := h.client.ListContacts(h.ctx(t), &kagamiv1.ListContactsRequest{Query: "LUMEN"})
	if err != nil || len(byQuery.GetContacts()) != 3 || byQuery.GetContacts()[0].GetCompanyName() != "Lumen" {
		t.Fatalf("query on company name: %d contacts, %v", len(byQuery.GetContacts()), err)
	}
	_, err = h.client.GetContact(h.ctxFor(t, "0198f000-0000-7000-8000-0000000000aa"), &kagamiv1.GetContactRequest{Id: moved.GetId()})
	requireStatus(t, err, codes.NotFound, "RESOURCE_NOT_FOUND")
}
