package connectapi

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
)

const (
	idempotencyKeyHeader = "Idempotency-Key"
	// boardPageSize and maxBoardPages bound how many jobs the board loads: all
	// non-archived jobs, which for one owner is far below the cap.
	boardPageSize = 200
	maxBoardPages = 50

	reasonInvalidMask = "UPDATE_MASK_INVALID"
)

// updatableJobFields are the job fields the browser may change with UpdateJob.
// Status moves go through ChangeJobStatus and the company is fixed at creation.
var updatableJobFields = map[string]struct{}{
	"title": {}, "url": {}, "source": {}, "location": {}, "salary_text": {},
	"description": {}, "applied_on": {}, "next_follow_up": {},
}

// JobsBackend is the part of kagami's client JobsServer uses.
type JobsBackend interface {
	ListJobs(ctx context.Context, in *kagamiv1.ListJobsRequest, opts ...grpc.CallOption) (*kagamiv1.ListJobsResponse, error)
	GetJob(ctx context.Context, in *kagamiv1.GetJobRequest, opts ...grpc.CallOption) (*kagamiv1.GetJobResponse, error)
	AddJob(ctx context.Context, in *kagamiv1.AddJobRequest, opts ...grpc.CallOption) (*kagamiv1.AddJobResponse, error)
	UpdateJob(ctx context.Context, in *kagamiv1.UpdateJobRequest, opts ...grpc.CallOption) (*kagamiv1.UpdateJobResponse, error)
	ChangeJobStatus(ctx context.Context, in *kagamiv1.ChangeJobStatusRequest, opts ...grpc.CallOption) (*kagamiv1.ChangeJobStatusResponse, error)
}

// JobsServer implements shogun.api.v1.JobsService on top of kagami.
type JobsServer struct {
	kagami JobsBackend
	log    *slog.Logger
}

var _ apiv1connect.JobsServiceHandler = (*JobsServer)(nil)

// NewJobsServer returns a JobsServer that calls kagami through backend.
func NewJobsServer(backend JobsBackend, log *slog.Logger) *JobsServer {
	return &JobsServer{kagami: backend, log: log}
}

// GetBoard returns every non-archived job grouped by status.
func (s *JobsServer) GetBoard(
	ctx context.Context, _ *connect.Request[apiv1.GetBoardRequest],
) (*connect.Response[apiv1.GetBoardResponse], error) {
	var jobs []*kagamiv1.Job
	token := ""
	for range maxBoardPages {
		resp, err := s.kagami.ListJobs(ctx, &kagamiv1.ListJobsRequest{PageSize: boardPageSize, PageToken: token})
		if err != nil {
			return nil, fromGRPC(ctx, s.log, err)
		}
		jobs = append(jobs, resp.GetJobs()...)
		if token = resp.GetNextPageToken(); token == "" {
			break
		}
	}
	return connect.NewResponse(&apiv1.GetBoardResponse{Columns: boardColumns(jobs)}), nil
}

// GetJob returns one job with its timeline.
func (s *JobsServer) GetJob(
	ctx context.Context, req *connect.Request[apiv1.GetJobRequest],
) (*connect.Response[apiv1.GetJobResponse], error) {
	resp, err := s.kagami.GetJob(ctx, &kagamiv1.GetJobRequest{Id: req.Msg.GetId()})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.GetJobResponse{
		Job:      jobToAPI(resp.GetJob(), resp.GetCompany()),
		Timeline: timelineToAPI(resp.GetEvents()),
	}), nil
}

// AddJob creates a job. A repeated Idempotency-Key returns the original job.
func (s *JobsServer) AddJob(
	ctx context.Context, req *connect.Request[apiv1.AddJobRequest],
) (*connect.Response[apiv1.AddJobResponse], error) {
	in := req.Msg
	resp, err := s.kagami.AddJob(ctx, &kagamiv1.AddJobRequest{
		Title:          in.GetTitle(),
		CompanyName:    in.GetCompanyName(),
		CompanyDomain:  in.GetCompanyDomain(),
		Url:            in.GetUrl(),
		Source:         in.GetSource(),
		Status:         jobStatusToKagami(in.GetStatus()),
		Location:       in.GetLocation(),
		SalaryText:     in.GetSalaryText(),
		Description:    in.GetDescription(),
		IdempotencyKey: req.Header().Get(idempotencyKeyHeader),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.AddJobResponse{Job: jobToAPI(resp.GetJob(), resp.GetCompany())}), nil
}

// UpdateJob changes the masked fields if the caller's version is current.
func (s *JobsServer) UpdateJob(
	ctx context.Context, req *connect.Request[apiv1.UpdateJobRequest],
) (*connect.Response[apiv1.UpdateJobResponse], error) {
	in := req.Msg
	if err := checkJobMask(in.GetUpdateMask()); err != nil {
		return nil, err
	}
	job := jobToKagami(in.GetJob())
	job.Id = in.GetJob().GetId()
	resp, err := s.kagami.UpdateJob(ctx, &kagamiv1.UpdateJobRequest{
		Job:        job,
		UpdateMask: in.GetUpdateMask(),
		Version:    in.GetVersion(),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.UpdateJobResponse{Job: jobToAPI(resp.GetJob(), nil)}), nil
}

// ChangeJobStatus moves a job along the state machine. An invalid move comes
// back as FailedPrecondition with reason JOB_STATUS_INVALID_TRANSITION.
func (s *JobsServer) ChangeJobStatus(
	ctx context.Context, req *connect.Request[apiv1.ChangeJobStatusRequest],
) (*connect.Response[apiv1.ChangeJobStatusResponse], error) {
	in := req.Msg
	resp, err := s.kagami.ChangeJobStatus(ctx, &kagamiv1.ChangeJobStatusRequest{
		Id:       in.GetId(),
		ToStatus: jobStatusToKagami(in.GetToStatus()),
		Note:     in.GetNote(),
		Version:  in.GetVersion(),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.ChangeJobStatusResponse{Job: jobToAPI(resp.GetJob(), nil)}), nil
}

// checkJobMask rejects an empty mask or one naming a field the browser may not change.
func checkJobMask(mask *fieldmaskpb.FieldMask) error {
	paths := mask.GetPaths()
	if len(paths) == 0 {
		return newError(connect.CodeInvalidArgument, reasonInvalidMask, "update_mask must name at least one field")
	}
	for _, path := range paths {
		if _, ok := updatableJobFields[path]; !ok {
			return newError(connect.CodeInvalidArgument, reasonInvalidMask, "field "+path+" cannot be updated")
		}
	}
	return nil
}
