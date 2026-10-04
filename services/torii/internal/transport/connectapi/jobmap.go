package connectapi

import (
	"sort"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
)

// jobStatusPairs maps the browser API's statuses to kagami's, in pipeline
// order. The board shows its columns in this order.
var jobStatusPairs = []struct {
	api    apiv1.JobStatus
	kagami kagamiv1.JobStatus
}{
	{apiv1.JobStatus_JOB_STATUS_SAVED, kagamiv1.JobStatus_JOB_STATUS_SAVED},
	{apiv1.JobStatus_JOB_STATUS_APPLIED, kagamiv1.JobStatus_JOB_STATUS_APPLIED},
	{apiv1.JobStatus_JOB_STATUS_SHORTLISTED, kagamiv1.JobStatus_JOB_STATUS_SHORTLISTED},
	{apiv1.JobStatus_JOB_STATUS_INTERVIEW, kagamiv1.JobStatus_JOB_STATUS_INTERVIEW},
	{apiv1.JobStatus_JOB_STATUS_OFFER, kagamiv1.JobStatus_JOB_STATUS_OFFER},
	{apiv1.JobStatus_JOB_STATUS_REJECTED, kagamiv1.JobStatus_JOB_STATUS_REJECTED},
}

func jobStatusToAPI(s kagamiv1.JobStatus) apiv1.JobStatus {
	for _, pair := range jobStatusPairs {
		if pair.kagami == s {
			return pair.api
		}
	}
	return apiv1.JobStatus_JOB_STATUS_UNSPECIFIED
}

func jobStatusToKagami(s apiv1.JobStatus) kagamiv1.JobStatus {
	for _, pair := range jobStatusPairs {
		if pair.api == s {
			return pair.kagami
		}
	}
	return kagamiv1.JobStatus_JOB_STATUS_UNSPECIFIED
}

// jobToAPI maps a kagami job. company supplies the domain and may be nil.
func jobToAPI(j *kagamiv1.Job, company *kagamiv1.Company) *apiv1.Job {
	return &apiv1.Job{
		Id:            j.GetId(),
		Title:         j.GetTitle(),
		CompanyName:   j.GetCompanyName(),
		CompanyDomain: company.GetDomain(),
		Status:        jobStatusToAPI(j.GetStatus()),
		Url:           j.GetUrl(),
		Source:        j.GetSource(),
		Location:      j.GetLocation(),
		SalaryText:    j.GetSalaryText(),
		Description:   j.GetDescription(),
		AppliedOn:     j.GetAppliedOn(),
		NextFollowUp:  j.GetNextFollowUp(),
		Version:       j.GetVersion(),
	}
}

func jobToKagami(j *apiv1.Job) *kagamiv1.Job {
	return &kagamiv1.Job{
		Id:           j.GetId(),
		Title:        j.GetTitle(),
		Url:          j.GetUrl(),
		Source:       j.GetSource(),
		Location:     j.GetLocation(),
		SalaryText:   j.GetSalaryText(),
		Description:  j.GetDescription(),
		AppliedOn:    j.GetAppliedOn(),
		NextFollowUp: j.GetNextFollowUp(),
	}
}

func jobCardToAPI(j *kagamiv1.Job) *apiv1.JobCard {
	return &apiv1.JobCard{
		Id:           j.GetId(),
		Title:        j.GetTitle(),
		CompanyName:  j.GetCompanyName(),
		Status:       jobStatusToAPI(j.GetStatus()),
		NextFollowUp: j.GetNextFollowUp(),
		Version:      j.GetVersion(),
	}
}

// boardColumns groups jobs into one column per status in pipeline order,
// keeping the order jobs arrived in. Empty columns are kept so the board
// always shows every stage.
func boardColumns(jobs []*kagamiv1.Job) []*apiv1.BoardColumn {
	columns := make([]*apiv1.BoardColumn, 0, len(jobStatusPairs))
	for _, pair := range jobStatusPairs {
		column := &apiv1.BoardColumn{Status: pair.api, Jobs: []*apiv1.JobCard{}}
		for _, j := range jobs {
			if j.GetStatus() == pair.kagami {
				column.Jobs = append(column.Jobs, jobCardToAPI(j))
			}
		}
		columns = append(columns, column)
	}
	return columns
}

// timelineToAPI maps job events, newest first.
func timelineToAPI(events []*kagamiv1.JobEvent) []*apiv1.TimelineEntry {
	sorted := append([]*kagamiv1.JobEvent(nil), events...)
	sort.SliceStable(sorted, func(i, j int) bool {
		return sorted[i].GetOccurredAt().AsTime().After(sorted[j].GetOccurredAt().AsTime())
	})
	entries := make([]*apiv1.TimelineEntry, 0, len(sorted))
	for _, e := range sorted {
		entries = append(entries, &apiv1.TimelineEntry{
			Id:         e.GetId(),
			Kind:       e.GetKind(),
			FromStatus: jobStatusToAPI(e.GetFromStatus()),
			ToStatus:   jobStatusToAPI(e.GetToStatus()),
			OccurredAt: e.GetOccurredAt(),
		})
	}
	return entries
}
