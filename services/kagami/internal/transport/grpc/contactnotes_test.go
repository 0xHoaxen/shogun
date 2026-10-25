package grpc_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/pkg/bus"
	"github.com/0xHoaxen/shogun/pkg/owners"
)

const otherOwner = "11111111-1111-4111-8111-111111111111"

func TestAddContactNoteStoresItOnTheTimelineAndWritesOneEvent(t *testing.T) {
	// Arrange
	h := newHarness(t)
	c := h.addContact(t, "Priya Raman", "priya@lumen.example", "")

	// Act
	res, err := h.client.AddContactNote(h.ctx(t), &kagamiv1.AddContactNoteRequest{Id: c.GetId(), Text: "  Met at GopherCon. Wants a referral note.  "})
	// Assert
	if err != nil {
		t.Fatalf("AddContactNote: %v", err)
	}
	if res.GetNote().GetKind() != "note" || !strings.Contains(res.GetNote().GetPayload(), `"text": "Met at GopherCon. Wants a referral note."`) {
		t.Fatalf("note = %+v, want kind note with the trimmed text", res.GetNote())
	}
	detail, err := h.client.GetContact(h.ctx(t), &kagamiv1.GetContactRequest{Id: c.GetId()})
	if err != nil || len(detail.GetTimeline()) != 2 || detail.GetTimeline()[0].GetId() != res.GetNote().GetId() {
		t.Fatalf("timeline = %+v, %v; want the note first, then created", detail.GetTimeline(), err)
	}
	if detail.GetContact().GetVersion() != c.GetVersion() {
		t.Errorf("version %d, want %d: a note does not change the contact", detail.GetContact().GetVersion(), c.GetVersion())
	}
	if n := h.outboxCount(t, "contact.note_added"); n != 1 {
		t.Fatalf("%d contact.note_added rows, want 1", n)
	}
}

func TestNoteEventsHaveNoConsumerYet(t *testing.T) {
	if got := bus.Consumers("contact.note_added"); got != nil {
		t.Fatalf("consumers = %v: a route needs a handler on the other side first", got)
	}
}

func TestAddContactNoteRefusesBadTextAndOtherPeoplesContacts(t *testing.T) {
	h := newHarness(t)
	c := h.addContact(t, "Priya Raman", "priya@lumen.example", "")
	tests := []struct {
		name   string
		owner  string
		id     string
		text   string
		code   codes.Code
		reason string
	}{
		{"empty", h.owner, c.GetId(), "", codes.InvalidArgument, "INVALID_NOTE"},
		{"only spaces", h.owner, c.GetId(), " \n\t ", codes.InvalidArgument, "INVALID_NOTE"},
		{"too long", h.owner, c.GetId(), strings.Repeat("a", 2001), codes.InvalidArgument, "NOTE_TOO_LONG"},
		{"not an id", h.owner, "nope", "hi", codes.InvalidArgument, "INVALID_ID"},
		{"no such contact", h.owner, uuid.NewString(), "hi", codes.NotFound, ""},
		{"someone else's contact", otherOwner, c.GetId(), "hi", codes.NotFound, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.AddContactNote(h.ctxFor(t, tt.owner), &kagamiv1.AddContactNoteRequest{Id: tt.id, Text: tt.text})

			requireStatus(t, err, tt.code, tt.reason)
		})
	}
	if n := h.outboxCount(t, "contact.note_added"); n != 0 {
		t.Fatalf("%d note events after only refusals, want 0", n)
	}
	// A note of exactly the limit is fine, counted in characters not bytes.
	if _, err := h.client.AddContactNote(h.ctx(t), &kagamiv1.AddContactNoteRequest{Id: c.GetId(), Text: strings.Repeat("é", 2000)}); err != nil {
		t.Fatalf("a note of 2000 characters: %v", err)
	}
}

func (h *harness) markContacted(t *testing.T, c *kagamiv1.Contact, on string) (*kagamiv1.MarkContactedResponse, error) {
	t.Helper()
	return h.client.MarkContacted(h.ctx(t), &kagamiv1.MarkContactedRequest{Id: c.GetId(), Version: c.GetVersion(), On: on})
}

func (h *harness) moveTo(t *testing.T, c *kagamiv1.Contact, to kagamiv1.ContactStatus) *kagamiv1.Contact {
	t.Helper()
	res, err := h.client.ChangeContactStatus(h.ctx(t), &kagamiv1.ChangeContactStatusRequest{Id: c.GetId(), Version: c.GetVersion(), ToStatus: to})
	if err != nil {
		t.Fatalf("move to %v: %v", to, err)
	}
	return res.GetContact()
}

func TestMarkContactedMovesAnUnreachedContactAndFiresTheStatusEvent(t *testing.T) {
	h := newHarness(t)
	c := h.addContact(t, "Priya Raman", "priya@lumen.example", "")

	res, err := h.markContacted(t, c, "2026-10-20")
	if err != nil {
		t.Fatalf("MarkContacted: %v", err)
	}
	got := res.GetContact()
	if got.GetStatus() != kagamiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT || got.GetLastContacted() != "2026-10-20" || got.GetVersion() != c.GetVersion()+1 {
		t.Fatalf("contact = %+v, want reached_out on 2026-10-20 at the next version", got)
	}
	if n := h.outboxCount(t, "contact.status_changed"); n != 1 {
		t.Fatalf("%d contact.status_changed rows, want 1: fude queues its follow-up from it", n)
	}
}

func TestMarkContactedOnAReachedContactOnlySetsTheDateAndNeverMovesBack(t *testing.T) {
	for name, to := range map[string]kagamiv1.ContactStatus{
		"reached out":          kagamiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT,
		"replied":              kagamiv1.ContactStatus_CONTACT_STATUS_REPLIED,
		"conversation started": kagamiv1.ContactStatus_CONTACT_STATUS_CONVERSATION_STARTED,
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			c := h.moveTo(t, h.addContact(t, "Priya Raman", "priya@lumen.example", ""), kagamiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT)
			if to != kagamiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT {
				c = h.moveTo(t, c, to)
			}
			before := h.outboxCount(t, "contact.status_changed")

			res, err := h.markContacted(t, c, "2026-10-21")
			if err != nil {
				t.Fatalf("MarkContacted: %v", err)
			}
			if res.GetContact().GetStatus() != to || res.GetContact().GetLastContacted() != "2026-10-21" {
				t.Fatalf("contact = %+v, want status %v kept and the date set", res.GetContact(), to)
			}
			if after := h.outboxCount(t, "contact.status_changed"); after != before {
				t.Fatalf("status events went from %d to %d, want no new one", before, after)
			}
			detail, _ := h.client.GetContact(h.ctx(t), &kagamiv1.GetContactRequest{Id: c.GetId()})
			if kind := detail.GetTimeline()[0].GetKind(); kind != "message_sent" {
				t.Fatalf("newest timeline entry is %q, want message_sent", kind)
			}
		})
	}
}

func TestMarkContactedChecksTheDateVersionAndOwner(t *testing.T) {
	h := newHarness(t)
	c := h.addContact(t, "Priya Raman", "priya@lumen.example", "")
	tomorrow := time.Now().UTC().AddDate(0, 0, 2).Format("2006-01-02")

	_, future := h.markContacted(t, c, tomorrow)
	_, garbage := h.markContacted(t, c, "last week")
	_, stale := h.client.MarkContacted(h.ctx(t), &kagamiv1.MarkContactedRequest{Id: c.GetId(), Version: c.GetVersion() + 7})
	_, noVersion := h.client.MarkContacted(h.ctx(t), &kagamiv1.MarkContactedRequest{Id: c.GetId()})
	_, stranger := h.client.MarkContacted(h.ctxFor(t, otherOwner), &kagamiv1.MarkContactedRequest{Id: c.GetId(), Version: c.GetVersion()})

	requireStatus(t, future, codes.InvalidArgument, "INVALID_DATE")
	requireStatus(t, garbage, codes.InvalidArgument, "INVALID_DATE")
	requireStatus(t, stale, codes.Aborted, "VERSION_CONFLICT")
	requireStatus(t, noVersion, codes.InvalidArgument, "VERSION_REQUIRED")
	requireStatus(t, stranger, codes.NotFound, "")
	detail, _ := h.client.GetContact(h.ctx(t), &kagamiv1.GetContactRequest{Id: c.GetId()})
	if detail.GetContact().GetStatus() != kagamiv1.ContactStatus_CONTACT_STATUS_NOT_REACHED {
		t.Fatalf("status %v after only refusals, want unchanged", detail.GetContact().GetStatus())
	}
}

func TestMarkContactedWithNoDateUsesTodayInTheOwnersTimeZone(t *testing.T) {
	// 23:30 UTC on 24 October is already the 25th in Auckland.
	now := time.Date(2026, 10, 24, 23, 30, 0, 0, time.UTC)
	h := newHarnessAt(t, func() time.Time { return now })
	if err := owners.Upsert(context.Background(), h.pool, owners.Owner{
		ID: uuid.MustParse(h.owner), Timezone: "Pacific/Auckland", Status: owners.StatusActive, Version: 1,
	}); err != nil {
		t.Fatalf("owner: %v", err)
	}
	c := h.addContact(t, "Priya Raman", "priya@lumen.example", "")

	res, err := h.markContacted(t, c, "")

	if err != nil || res.GetContact().GetLastContacted() != "2026-10-25" {
		t.Fatalf("res %+v, err %v; want the owner's today, 2026-10-25", res.GetContact(), err)
	}
	// The owner's tomorrow is still the future, and today is allowed.
	next, _ := h.client.GetContact(h.ctx(t), &kagamiv1.GetContactRequest{Id: c.GetId()})
	_, tomorrow := h.markContacted(t, next.GetContact(), "2026-10-26")
	requireStatus(t, tomorrow, codes.InvalidArgument, "INVALID_DATE")
}

func TestMarkContactedFallsBackToUTCForAnOwnerKagamiHasNotHeardOf(t *testing.T) {
	now := time.Date(2026, 10, 24, 23, 30, 0, 0, time.UTC)
	h := newHarnessAt(t, func() time.Time { return now })
	c := h.addContact(t, "Priya Raman", "priya@lumen.example", "")

	res, err := h.markContacted(t, c, "")

	if err != nil || res.GetContact().GetLastContacted() != "2026-10-24" {
		t.Fatalf("res %+v, err %v; want 2026-10-24 in UTC", res.GetContact(), err)
	}
}

func TestGetContactListsTheOwnersJobsAtTheContactsCompanyOnly(t *testing.T) {
	// Arrange: two jobs at Lumen for this owner, one at another company, one at
	// Lumen for someone else.
	h := newHarness(t)
	c := h.addContact(t, "Priya Raman", "priya@lumen.example", "")
	for _, title := range []string{"Backend Engineer", "Platform Engineer"} {
		if _, err := h.client.AddJob(h.ctx(t), &kagamiv1.AddJobRequest{Title: title, CompanyName: "Lumen", Url: "https://lumen.example/" + title}); err != nil {
			t.Fatalf("add job: %v", err)
		}
	}
	if _, err := h.client.AddJob(h.ctx(t), &kagamiv1.AddJobRequest{Title: "SRE", CompanyName: "Northwind", Url: "https://northwind.example/sre"}); err != nil {
		t.Fatalf("add job: %v", err)
	}
	if _, err := h.client.AddJob(h.ctxFor(t, otherOwner), &kagamiv1.AddJobRequest{Title: "Their Lumen job", CompanyName: "Lumen", Url: "https://lumen.example/theirs"}); err != nil {
		t.Fatalf("add job: %v", err)
	}

	// Act
	detail, err := h.client.GetContact(h.ctx(t), &kagamiv1.GetContactRequest{Id: c.GetId()})
	// Assert
	if err != nil {
		t.Fatalf("GetContact: %v", err)
	}
	var titles []string
	for _, j := range detail.GetLinkedJobs() {
		titles = append(titles, j.GetTitle())
		if j.GetCompanyName() != "Lumen" {
			t.Errorf("job %q shows company %q, want Lumen", j.GetTitle(), j.GetCompanyName())
		}
	}
	if len(titles) != 2 || titles[0] != "Platform Engineer" || titles[1] != "Backend Engineer" {
		t.Fatalf("linked jobs = %v, want the two Lumen jobs, newest first", titles)
	}
}

func TestAContactWithNoCompanyHasNoLinkedJobs(t *testing.T) {
	h := newHarness(t)
	res, err := h.client.AddContact(h.ctx(t), &kagamiv1.AddContactRequest{Contact: &kagamiv1.Contact{FullName: "Solo Person", Email: "solo@example.com"}})
	if err != nil {
		t.Fatalf("add contact: %v", err)
	}

	detail, err := h.client.GetContact(h.ctx(t), &kagamiv1.GetContactRequest{Id: res.GetContact().GetId()})

	if err != nil || len(detail.GetLinkedJobs()) != 0 {
		t.Fatalf("linked jobs = %v, err %v; want none", detail.GetLinkedJobs(), err)
	}
}
