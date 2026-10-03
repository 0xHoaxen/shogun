package grpc_test

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
)

func (h *harness) addJob(t *testing.T, title, key string) *kagamiv1.AddJobResponse {
	t.Helper()
	res, err := h.client.AddJob(h.ctx(t), &kagamiv1.AddJobRequest{
		Title: title, CompanyName: "Northwind", CompanyDomain: "northwind.example",
		Url: "https://northwind.example/jobs/" + title, IdempotencyKey: key,
	})
	if err != nil {
		t.Fatalf("add job: %v", err)
	}
	return res
}

func TestCallsWithoutAValidOwnerAreRefused(t *testing.T) {
	h := newHarness(t)

	_, err := h.client.ListJobs(t.Context(), &kagamiv1.ListJobsRequest{})
	requireStatus(t, err, codes.Unauthenticated, "")

	// A relay identity such as system:kagami is signed but is not an owner.
	_, err = h.client.ListJobs(h.ctxFor(t, "system:kagami"), &kagamiv1.ListJobsRequest{})
	requireStatus(t, err, codes.PermissionDenied, "OWNER_REQUIRED")
}

func TestAddJobWritesOneOutboxEvent(t *testing.T) {
	h := newHarness(t)

	res := h.addJob(t, "Backend Engineer", "")

	if res.GetJob().GetStatus() != kagamiv1.JobStatus_JOB_STATUS_SAVED || res.GetJob().GetVersion() != 1 {
		t.Fatalf("got status %s version %d", res.GetJob().GetStatus(), res.GetJob().GetVersion())
	}
	if res.GetCompany().GetDomain() != "northwind.example" || res.GetJob().GetCompanyId() != res.GetCompany().GetId() {
		t.Fatalf("company not linked: %+v", res)
	}
	if n := h.outboxCount(t, "job.added"); n != 1 {
		t.Fatalf("got %d job.added rows, want 1", n)
	}
	if n := h.outboxCount(t, ""); n != 1 {
		t.Fatalf("got %d outbox rows, want 1", n)
	}
}

func TestAddJobReusesCompanyByDomain(t *testing.T) {
	h := newHarness(t)

	first := h.addJob(t, "Backend Engineer", "")
	second := h.addJob(t, "Platform Engineer", "")

	if first.GetCompany().GetId() != second.GetCompany().GetId() {
		t.Fatalf("got two companies for one domain")
	}
}

func TestAddJobReplaysIdempotencyKeyWithoutANewEvent(t *testing.T) {
	h := newHarness(t)

	first := h.addJob(t, "SRE", "add-sre-1")
	again := h.addJob(t, "SRE", "add-sre-1")

	if first.GetJob().GetId() != again.GetJob().GetId() || again.GetCompany().GetName() != "Northwind" {
		t.Fatalf("replay returned %+v, want job %s", again, first.GetJob().GetId())
	}
	if n := h.outboxCount(t, ""); n != 1 {
		t.Fatalf("got %d outbox rows after a replay, want 1", n)
	}
}

func TestAddJobRejectsBadInput(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		name   string
		req    *kagamiv1.AddJobRequest
		reason string
	}{
		{"no title", &kagamiv1.AddJobRequest{CompanyName: "Lumen"}, "TITLE_REQUIRED"},
		{"no company", &kagamiv1.AddJobRequest{Title: "SRE"}, "COMPANY_REQUIRED"},
		{"bad source", &kagamiv1.AddJobRequest{Title: "SRE", CompanyName: "Lumen", Source: "carrier-pigeon"}, "INVALID_SOURCE"},
		{"unknown status", &kagamiv1.AddJobRequest{Title: "SRE", CompanyName: "Lumen", Status: kagamiv1.JobStatus(42)}, "INVALID_STATUS"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.AddJob(h.ctx(t), tt.req)
			requireStatus(t, err, codes.InvalidArgument, tt.reason)
		})
	}
	if n := h.outboxCount(t, ""); n != 0 {
		t.Fatalf("got %d outbox rows after failed calls, want 0", n)
	}
}

func TestAddJobWithADuplicateURLIsRefused(t *testing.T) {
	h := newHarness(t)
	h.addJob(t, "Backend Engineer", "")

	_, err := h.client.AddJob(h.ctx(t), &kagamiv1.AddJobRequest{
		Title: "Backend Engineer again", CompanyName: "Northwind",
		Url: "https://northwind.example/jobs/Backend Engineer",
	})

	requireStatus(t, err, codes.AlreadyExists, "ALREADY_EXISTS")
	if n := h.outboxCount(t, ""); n != 1 {
		t.Fatalf("got %d outbox rows, want only the first job's", n)
	}
}

func TestChangeJobStatusFollowsTheStateMachine(t *testing.T) {
	h := newHarness(t)
	job := h.addJob(t, "Backend Engineer", "").GetJob()

	moved, err := h.client.ChangeJobStatus(h.ctx(t), &kagamiv1.ChangeJobStatusRequest{
		Id: job.GetId(), ToStatus: kagamiv1.JobStatus_JOB_STATUS_APPLIED, Version: job.GetVersion(), Note: "via referral",
	})
	if err != nil {
		t.Fatalf("saved to applied: %v", err)
	}
	if moved.GetJob().GetStatus() != kagamiv1.JobStatus_JOB_STATUS_APPLIED ||
		moved.GetJob().GetVersion() != 2 || moved.GetJob().GetAppliedOn() == "" {
		t.Fatalf("got %+v", moved.GetJob())
	}
	if n := h.outboxCount(t, "job.status_changed"); n != 1 {
		t.Fatalf("got %d job.status_changed rows, want 1", n)
	}

	// applied to saved is not an edge of the state machine.
	_, err = h.client.ChangeJobStatus(h.ctx(t), &kagamiv1.ChangeJobStatusRequest{
		Id: job.GetId(), ToStatus: kagamiv1.JobStatus_JOB_STATUS_SAVED, Version: moved.GetJob().GetVersion(),
	})
	requireStatus(t, err, codes.FailedPrecondition, "JOB_STATUS_INVALID_TRANSITION")

	// The first call's version is stale now.
	_, err = h.client.ChangeJobStatus(h.ctx(t), &kagamiv1.ChangeJobStatusRequest{
		Id: job.GetId(), ToStatus: kagamiv1.JobStatus_JOB_STATUS_REJECTED, Version: job.GetVersion(),
	})
	requireStatus(t, err, codes.Aborted, "VERSION_CONFLICT")

	if n := h.outboxCount(t, "job.status_changed"); n != 1 {
		t.Fatalf("failed calls left %d job.status_changed rows, want 1", n)
	}
	detail, err := h.client.GetJob(h.ctx(t), &kagamiv1.GetJobRequest{Id: job.GetId()})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if len(detail.GetEvents()) != 2 || detail.GetEvents()[0].GetKind() != "status_changed" {
		t.Fatalf("got timeline %+v, want created and status_changed", detail.GetEvents())
	}
}

func TestChangeJobStatusRejectsBadInput(t *testing.T) {
	h := newHarness(t)
	job := h.addJob(t, "Backend Engineer", "").GetJob()
	tests := []struct {
		name   string
		req    *kagamiv1.ChangeJobStatusRequest
		code   codes.Code
		reason string
	}{
		{"no target status", &kagamiv1.ChangeJobStatusRequest{Id: job.GetId(), Version: 1}, codes.InvalidArgument, "INVALID_STATUS"},
		{"no version", &kagamiv1.ChangeJobStatusRequest{Id: job.GetId(), ToStatus: kagamiv1.JobStatus_JOB_STATUS_APPLIED}, codes.InvalidArgument, "VERSION_REQUIRED"},
		{"bad id", &kagamiv1.ChangeJobStatusRequest{Id: "nope", ToStatus: kagamiv1.JobStatus_JOB_STATUS_APPLIED, Version: 1}, codes.InvalidArgument, "INVALID_ID"},
		{"unknown job", &kagamiv1.ChangeJobStatusRequest{Id: "0198f000-0000-7000-8000-000000000000", ToStatus: kagamiv1.JobStatus_JOB_STATUS_APPLIED, Version: 1}, codes.NotFound, "RESOURCE_NOT_FOUND"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.ChangeJobStatus(h.ctx(t), tt.req)
			requireStatus(t, err, tt.code, tt.reason)
		})
	}
}

func TestOtherOwnersCannotSeeAJob(t *testing.T) {
	h := newHarness(t)
	job := h.addJob(t, "Backend Engineer", "").GetJob()

	_, err := h.client.GetJob(h.ctxFor(t, "0198f000-0000-7000-8000-0000000000aa"), &kagamiv1.GetJobRequest{Id: job.GetId()})

	requireStatus(t, err, codes.NotFound, "RESOURCE_NOT_FOUND")
}

func TestUpdateJobAppliesTheMaskAndWritesNoOutboxEvent(t *testing.T) {
	h := newHarness(t)
	job := h.addJob(t, "Backend Engineer", "").GetJob()
	before := h.outboxCount(t, "")

	res, err := h.client.UpdateJob(h.ctx(t), &kagamiv1.UpdateJobRequest{
		Job:        &kagamiv1.Job{Id: job.GetId(), Title: "Senior Backend Engineer", NextFollowUp: "2026-10-10", Location: "ignored"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title", "next_follow_up"}},
		Version:    job.GetVersion(),
	})
	if err != nil {
		t.Fatalf("update: %v", err)
	}
	got := res.GetJob()
	if got.GetTitle() != "Senior Backend Engineer" || got.GetNextFollowUp() != "2026-10-10" ||
		got.GetLocation() != "" || got.GetVersion() != job.GetVersion()+1 {
		t.Fatalf("got %+v", got)
	}
	if n := h.outboxCount(t, ""); n != before {
		t.Fatalf("update wrote %d outbox rows, want none", n-before)
	}

	detail, err := h.client.GetJob(h.ctx(t), &kagamiv1.GetJobRequest{Id: job.GetId()})
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if kind := detail.GetEvents()[0].GetKind(); kind != "follow_up_set" {
		t.Fatalf("newest timeline entry is %q, want follow_up_set", kind)
	}

	_, err = h.client.UpdateJob(h.ctx(t), &kagamiv1.UpdateJobRequest{
		Job:        &kagamiv1.Job{Id: job.GetId(), Title: "Stale"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"title"}},
		Version:    job.GetVersion(),
	})
	requireStatus(t, err, codes.Aborted, "VERSION_CONFLICT")
}

func TestUpdateJobRefusesStatusAndUnknownFields(t *testing.T) {
	h := newHarness(t)
	job := h.addJob(t, "Backend Engineer", "").GetJob()
	tests := []struct {
		name   string
		paths  []string
		reason string
	}{
		{"status", []string{"status"}, "STATUS_NOT_EDITABLE"},
		{"unknown field", []string{"company_id"}, "UNKNOWN_FIELD"},
		{"empty mask", nil, "UPDATE_MASK_REQUIRED"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.UpdateJob(h.ctx(t), &kagamiv1.UpdateJobRequest{
				Job:        &kagamiv1.Job{Id: job.GetId()},
				UpdateMask: &fieldmaskpb.FieldMask{Paths: tt.paths},
				Version:    job.GetVersion(),
			})
			requireStatus(t, err, codes.InvalidArgument, tt.reason)
		})
	}
}

func TestListJobsFiltersAndPaginates(t *testing.T) {
	h := newHarness(t)
	for _, title := range []string{"A", "B", "C"} {
		h.addJob(t, title, "")
	}
	applied := h.addJob(t, "D", "").GetJob()
	if _, err := h.client.ChangeJobStatus(h.ctx(t), &kagamiv1.ChangeJobStatusRequest{
		Id: applied.GetId(), ToStatus: kagamiv1.JobStatus_JOB_STATUS_APPLIED, Version: applied.GetVersion(),
	}); err != nil {
		t.Fatalf("change status: %v", err)
	}

	first, err := h.client.ListJobs(h.ctx(t), &kagamiv1.ListJobsRequest{
		Status: kagamiv1.JobStatus_JOB_STATUS_SAVED, PageSize: 2,
	})
	if err != nil || len(first.GetJobs()) != 2 || first.GetNextPageToken() == "" {
		t.Fatalf("first page: %d jobs, token %q, %v", len(first.GetJobs()), first.GetNextPageToken(), err)
	}
	second, err := h.client.ListJobs(h.ctx(t), &kagamiv1.ListJobsRequest{
		Status: kagamiv1.JobStatus_JOB_STATUS_SAVED, PageSize: 2, PageToken: first.GetNextPageToken(),
	})
	if err != nil || len(second.GetJobs()) != 1 || second.GetNextPageToken() != "" {
		t.Fatalf("second page: %d jobs, token %q, %v", len(second.GetJobs()), second.GetNextPageToken(), err)
	}

	_, err = h.client.ListJobs(h.ctx(t), &kagamiv1.ListJobsRequest{PageToken: "garbage"})
	requireStatus(t, err, codes.InvalidArgument, "INVALID_PAGE_TOKEN")
	_, err = h.client.ListJobs(h.ctx(t), &kagamiv1.ListJobsRequest{DueBefore: "yesterday"})
	requireStatus(t, err, codes.InvalidArgument, "INVALID_DATE")
}
