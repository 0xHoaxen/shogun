package grpc_test

import (
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
)

func (h *harness) links(t *testing.T, from string, urls ...string) *kagamiv1.FindMailLinksResponse {
	t.Helper()
	res, err := h.client.FindMailLinks(h.ctx(t), &kagamiv1.FindMailLinksRequest{FromEmail: from, Urls: urls})
	if err != nil {
		t.Fatalf("find links: %v", err)
	}
	return res
}

func TestFindMailLinksFindsTheContactBySenderAddressIgnoringCase(t *testing.T) {
	h := newHarness(t)
	c := h.addContact(t, "Priya Raman", "priya@lumen.example", "")

	got := h.links(t, "Priya Raman <PRIYA@Lumen.example>")

	if got.GetContactId() != c.GetId() || got.GetJobId() != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestFindMailLinksFindsTheJobByPostingURL(t *testing.T) {
	h := newHarness(t)
	job := h.addJob(t, "Backend", "").GetJob()

	got := h.links(t, "noreply@elsewhere.example", "https://other.example/x", job.GetUrl())

	if got.GetJobId() != job.GetId() || got.GetContactId() != "" {
		t.Fatalf("got %+v", got)
	}
}

func TestFindMailLinksFindsTheJobBySenderCompanyDomainIncludingSubdomains(t *testing.T) {
	h := newHarness(t)
	job := h.addJob(t, "Backend", "").GetJob() // company domain northwind.example

	for _, from := range []string{"hr@northwind.example", "talent@mail.eu.northwind.example"} {
		if got := h.links(t, from); got.GetJobId() != job.GetId() {
			t.Errorf("from %s: got %+v, want job %s", from, got, job.GetId())
		}
	}
	if got := h.links(t, "hr@unknown.example"); got.GetJobId() != "" {
		t.Errorf("an unknown domain matched %+v", got)
	}
}

func TestFindMailLinksPrefersAnOpenJobAndAURLOverTheDomain(t *testing.T) {
	h := newHarness(t)
	rejected := h.addJob(t, "Old", "").GetJob()
	open := h.addJob(t, "New", "").GetJob()
	if _, err := h.client.ChangeJobStatus(h.ctx(t), &kagamiv1.ChangeJobStatusRequest{
		Id: rejected.GetId(), ToStatus: kagamiv1.JobStatus_JOB_STATUS_REJECTED, Version: rejected.GetVersion(),
	}); err != nil {
		t.Fatalf("reject: %v", err)
	}

	byDomain := h.links(t, "hr@northwind.example")
	byURL := h.links(t, "hr@northwind.example", rejected.GetUrl())

	if byDomain.GetJobId() != open.GetId() {
		t.Errorf("by domain: got %s, want the open job %s", byDomain.GetJobId(), open.GetId())
	}
	if byURL.GetJobId() != rejected.GetId() {
		t.Errorf("by URL: got %s, want the job named in the mail %s", byURL.GetJobId(), rejected.GetId())
	}
}

func TestFindMailLinksNeverMatchesACompanyByAFreeMailDomain(t *testing.T) {
	h := newHarness(t)
	if _, err := h.client.AddJob(h.ctx(t), &kagamiv1.AddJobRequest{
		Title: "Odd", CompanyName: "Gmail Co", CompanyDomain: "gmail.com", Url: "https://gmail.com/jobs/1",
	}); err != nil {
		t.Fatalf("add job: %v", err)
	}

	got := h.links(t, "someone@gmail.com")

	if got.GetJobId() != "" {
		t.Fatalf("a free-mail sender matched job %s", got.GetJobId())
	}
}

func TestFindMailLinksFallsBackToTheContactsOwnJob(t *testing.T) {
	h := newHarness(t)
	job := h.addJob(t, "Backend", "").GetJob()
	added, err := h.client.AddContact(h.ctx(t), &kagamiv1.AddContactRequest{
		Contact: &kagamiv1.Contact{FullName: "Sam Lee", Email: "sam@friend.example", JobId: job.GetId()}, CompanyName: "Friend Co",
	})
	if err != nil {
		t.Fatalf("add contact: %v", err)
	}

	got := h.links(t, "sam@friend.example")

	if got.GetContactId() != added.GetContact().GetId() || got.GetJobId() != job.GetId() {
		t.Fatalf("got %+v", got)
	}
}

func TestFindMailLinksIsPerOwnerAndNeedsAnOwner(t *testing.T) {
	h := newHarness(t)
	h.addContact(t, "Priya Raman", "priya@lumen.example", "")
	h.addJob(t, "Backend", "")
	stranger := h.ctxFor(t, uuid.NewString())

	res, err := h.client.FindMailLinks(stranger, &kagamiv1.FindMailLinksRequest{FromEmail: "priya@northwind.example"})
	_, noOwner := h.client.FindMailLinks(h.ctxFor(t, "not-a-uuid"), &kagamiv1.FindMailLinksRequest{FromEmail: "a@b.example"})

	if err != nil || res.GetContactId() != "" || res.GetJobId() != "" {
		t.Fatalf("another owner saw %+v, %v", res, err)
	}
	requireStatus(t, noOwner, codes.PermissionDenied, "OWNER_REQUIRED")
}
