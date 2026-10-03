package grpc

import (
	"context"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/services/kagami/internal/app"
	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
	"github.com/0xHoaxen/shogun/services/kagami/internal/wire"
)

// optionalJobStatus reads a status filter or default: unspecified means none.
func optionalJobStatus(p kagamiv1.JobStatus) (domain.JobStatus, error) {
	if p == kagamiv1.JobStatus_JOB_STATUS_UNSPECIFIED {
		return "", nil
	}
	return requiredJobStatus(p)
}

func requiredJobStatus(p kagamiv1.JobStatus) (domain.JobStatus, error) {
	s, ok := wire.JobStatusFromProto(p)
	if !ok {
		return "", &app.InvalidArgumentError{Reason: "INVALID_STATUS", Msg: "status is missing or unknown"}
	}
	return s, nil
}

// AddJob implements kagami.v1.KagamiService.
func (s *Server) AddJob(ctx context.Context, req *kagamiv1.AddJobRequest) (*kagamiv1.AddJobResponse, error) {
	status, err := optionalJobStatus(req.GetStatus())
	if err != nil {
		return nil, toStatus(err)
	}
	res, err := s.svc.AddJob(ctx, app.AddJobInput{
		Title: req.GetTitle(), CompanyName: req.GetCompanyName(), CompanyDomain: req.GetCompanyDomain(),
		URL: req.GetUrl(), Source: req.GetSource(), Status: status, Location: req.GetLocation(),
		SalaryText: req.GetSalaryText(), Description: req.GetDescription(), IdempotencyKey: req.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &kagamiv1.AddJobResponse{Job: jobToProto(res.Job), Company: companyToProto(res.Company)}, nil
}

// GetJob implements kagami.v1.KagamiService.
func (s *Server) GetJob(ctx context.Context, req *kagamiv1.GetJobRequest) (*kagamiv1.GetJobResponse, error) {
	detail, err := s.svc.GetJob(ctx, req.GetId())
	if err != nil {
		return nil, toStatus(err)
	}
	events := make([]*kagamiv1.JobEvent, 0, len(detail.Events))
	for _, e := range detail.Events {
		events = append(events, jobEventToProto(e))
	}
	return &kagamiv1.GetJobResponse{
		Job: jobToProto(detail.Job), Company: companyToProto(detail.Company), Events: events,
	}, nil
}

// ListJobs implements kagami.v1.KagamiService.
func (s *Server) ListJobs(ctx context.Context, req *kagamiv1.ListJobsRequest) (*kagamiv1.ListJobsResponse, error) {
	status, err := optionalJobStatus(req.GetStatus())
	if err != nil {
		return nil, toStatus(err)
	}
	res, err := s.svc.ListJobs(ctx, app.ListJobsInput{
		Status: status, CompanyID: req.GetCompanyId(), DueBefore: req.GetDueBefore(),
		PageSize: req.GetPageSize(), PageToken: req.GetPageToken(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	jobs := make([]*kagamiv1.Job, 0, len(res.Jobs))
	for _, j := range res.Jobs {
		jobs = append(jobs, jobToProto(j))
	}
	return &kagamiv1.ListJobsResponse{Jobs: jobs, NextPageToken: res.NextPageToken}, nil
}

// UpdateJob implements kagami.v1.KagamiService.
func (s *Server) UpdateJob(ctx context.Context, req *kagamiv1.UpdateJobRequest) (*kagamiv1.UpdateJobResponse, error) {
	j := req.GetJob()
	job, err := s.svc.UpdateJob(ctx, app.UpdateJobInput{
		ID:      j.GetId(),
		Version: req.GetVersion(),
		Paths:   req.GetUpdateMask().GetPaths(),
		Fields: app.JobFields{
			Title: j.GetTitle(), URL: j.GetUrl(), Source: j.GetSource(), Location: j.GetLocation(),
			SalaryText: j.GetSalaryText(), Description: j.GetDescription(),
			AppliedOn: j.GetAppliedOn(), NextFollowUp: j.GetNextFollowUp(),
		},
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &kagamiv1.UpdateJobResponse{Job: jobToProto(job)}, nil
}

// ChangeJobStatus implements kagami.v1.KagamiService.
func (s *Server) ChangeJobStatus(ctx context.Context, req *kagamiv1.ChangeJobStatusRequest) (*kagamiv1.ChangeJobStatusResponse, error) {
	to, err := requiredJobStatus(req.GetToStatus())
	if err != nil {
		return nil, toStatus(err)
	}
	job, err := s.svc.ChangeJobStatus(ctx, app.ChangeJobStatusInput{
		ID: req.GetId(), To: to, Note: req.GetNote(), Version: req.GetVersion(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &kagamiv1.ChangeJobStatusResponse{Job: jobToProto(job)}, nil
}
