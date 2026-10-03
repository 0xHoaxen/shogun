package grpc_test

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
)

func TestListDueFollowUpsReturnsItemsOnOrBeforeTheDate(t *testing.T) {
	h := newHarness(t)
	overdue := h.addJob(t, "Overdue", "").GetJob()
	today := h.addJob(t, "Today", "").GetJob()
	later := h.addJob(t, "Later", "").GetJob()
	h.addJob(t, "No follow-up", "")
	for job, day := range map[*kagamiv1.Job]string{overdue: "2026-10-01", today: "2026-10-03", later: "2026-10-09"} {
		_, err := h.client.UpdateJob(h.ctx(t), &kagamiv1.UpdateJobRequest{
			Job:        &kagamiv1.Job{Id: job.GetId(), NextFollowUp: day},
			UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"next_follow_up"}},
			Version:    job.GetVersion(),
		})
		if err != nil {
			t.Fatalf("set follow-up %s: %v", day, err)
		}
	}
	contact := h.addContact(t, "Arjun Mehta", "arjun@corvid.example", "")
	if _, err := h.client.UpdateContact(h.ctx(t), &kagamiv1.UpdateContactRequest{
		Contact:    &kagamiv1.Contact{Id: contact.GetId(), NextFollowUp: "2026-10-03"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"next_follow_up"}},
		Version:    contact.GetVersion(),
	}); err != nil {
		t.Fatalf("set contact follow-up: %v", err)
	}

	res, err := h.client.ListDueFollowUps(h.ctx(t), &kagamiv1.ListDueFollowUpsRequest{OnOrBefore: "2026-10-03"})
	if err != nil {
		t.Fatalf("list due: %v", err)
	}

	if len(res.GetJobs()) != 2 || res.GetJobs()[0].GetTitle() != "Overdue" || res.GetJobs()[1].GetTitle() != "Today" {
		t.Fatalf("got jobs %+v, want Overdue then Today", res.GetJobs())
	}
	if len(res.GetContacts()) != 1 || res.GetContacts()[0].GetFullName() != "Arjun Mehta" {
		t.Fatalf("got contacts %+v", res.GetContacts())
	}
}

func TestListDueFollowUpsSkipsRejectedJobsAndValidatesTheDate(t *testing.T) {
	h := newHarness(t)
	job := h.addJob(t, "Rejected role", "").GetJob()
	rejected, err := h.client.ChangeJobStatus(h.ctx(t), &kagamiv1.ChangeJobStatusRequest{
		Id: job.GetId(), ToStatus: kagamiv1.JobStatus_JOB_STATUS_REJECTED, Version: job.GetVersion(),
	})
	if err != nil {
		t.Fatalf("reject: %v", err)
	}
	if _, err := h.client.UpdateJob(h.ctx(t), &kagamiv1.UpdateJobRequest{
		Job:        &kagamiv1.Job{Id: job.GetId(), NextFollowUp: "2026-10-01"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"next_follow_up"}},
		Version:    rejected.GetJob().GetVersion(),
	}); err != nil {
		t.Fatalf("set follow-up: %v", err)
	}

	res, err := h.client.ListDueFollowUps(h.ctx(t), &kagamiv1.ListDueFollowUpsRequest{OnOrBefore: "2026-10-03"})
	if err != nil || len(res.GetJobs()) != 0 {
		t.Fatalf("got %d jobs, %v; a rejected job needs no follow-up", len(res.GetJobs()), err)
	}

	_, err = h.client.ListDueFollowUps(h.ctx(t), &kagamiv1.ListDueFollowUpsRequest{OnOrBefore: "soon"})
	requireStatus(t, err, codes.InvalidArgument, "INVALID_DATE")
}

func TestListDueFollowUpsDefaultsToToday(t *testing.T) {
	h := newHarness(t)

	res, err := h.client.ListDueFollowUps(h.ctx(t), &kagamiv1.ListDueFollowUpsRequest{})

	if err != nil || len(res.GetJobs()) != 0 || len(res.GetContacts()) != 0 {
		t.Fatalf("got %+v, %v", res, err)
	}
}
