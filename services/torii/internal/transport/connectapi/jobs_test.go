package connectapi_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/fieldmaskpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/app/apptest"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/connectapi"
)

// fakeKagami answers kagami calls from the funcs a test sets, and records the
// owner each call acted for and the requests it received.
type fakeKagami struct {
	listJobs        func(*kagamiv1.ListJobsRequest) (*kagamiv1.ListJobsResponse, error)
	getJob          func(*kagamiv1.GetJobRequest) (*kagamiv1.GetJobResponse, error)
	addJob          func(*kagamiv1.AddJobRequest) (*kagamiv1.AddJobResponse, error)
	updateJob       func(*kagamiv1.UpdateJobRequest) (*kagamiv1.UpdateJobResponse, error)
	changeJobStatus func(*kagamiv1.ChangeJobStatusRequest) (*kagamiv1.ChangeJobStatusResponse, error)

	mu       sync.Mutex
	owners   []string
	requests []any
}

func (f *fakeKagami) record(ctx context.Context, in any) {
	id, _ := authz.FromContext(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owners = append(f.owners, id.OwnerID)
	f.requests = append(f.requests, in)
}

func (f *fakeKagami) ownersSeen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.owners...)
}

func (f *fakeKagami) requestsSeen() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]any(nil), f.requests...)
}

func (f *fakeKagami) ListJobs(ctx context.Context, in *kagamiv1.ListJobsRequest, _ ...grpc.CallOption) (*kagamiv1.ListJobsResponse, error) {
	f.record(ctx, in)
	return f.listJobs(in)
}

func (f *fakeKagami) GetJob(ctx context.Context, in *kagamiv1.GetJobRequest, _ ...grpc.CallOption) (*kagamiv1.GetJobResponse, error) {
	f.record(ctx, in)
	return f.getJob(in)
}

func (f *fakeKagami) AddJob(ctx context.Context, in *kagamiv1.AddJobRequest, _ ...grpc.CallOption) (*kagamiv1.AddJobResponse, error) {
	f.record(ctx, in)
	return f.addJob(in)
}

func (f *fakeKagami) UpdateJob(ctx context.Context, in *kagamiv1.UpdateJobRequest, _ ...grpc.CallOption) (*kagamiv1.UpdateJobResponse, error) {
	f.record(ctx, in)
	return f.updateJob(in)
}

func (f *fakeKagami) ChangeJobStatus(ctx context.Context, in *kagamiv1.ChangeJobStatusRequest, _ ...grpc.CallOption) (*kagamiv1.ChangeJobStatusResponse, error) {
	f.record(ctx, in)
	return f.changeJobStatus(in)
}

type jobsHarness struct {
	kagami *fakeKagami
	client apiv1connect.JobsServiceClient
	token  string
	owner  string
}

func newJobsHarness(t *testing.T) *jobsHarness {
	t.Helper()
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	auth, err := app.NewAuth(apptest.NewMemSessions(),
		app.AuthConfig{AllowedEmails: []string{ownerEmail}, SessionTTL: sessionTTL},
		app.WithClock(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	token, session, err := auth.StartSession(context.Background(),
		app.Claims{Subject: "sub-1", Email: ownerEmail, EmailVerified: true})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	log := slog.New(slog.DiscardHandler)
	kagami := &fakeKagami{}
	mux := http.NewServeMux()
	mux.Handle(apiv1connect.NewJobsServiceHandler(connectapi.NewJobsServer(kagami, log),
		connect.WithInterceptors(connectapi.NewSessionInterceptor(connectapi.InterceptorConfig{Auth: auth, Log: log}))))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &jobsHarness{
		kagami: kagami, token: token, owner: session.OwnerID.String(),
		client: apiv1connect.NewJobsServiceClient(srv.Client(), srv.URL),
	}
}

func kagamiJob(id, title, company string, s kagamiv1.JobStatus) *kagamiv1.Job {
	return &kagamiv1.Job{Id: id, Title: title, CompanyName: company, Status: s, Version: 3, NextFollowUp: "2026-10-08"}
}

func grpcError(code codes.Code, reason, message string) error {
	st, err := status.New(code, message).WithDetails(&errdetails.ErrorInfo{Reason: reason, Domain: "shogun"})
	if err != nil {
		panic(err)
	}
	return st.Err()
}

func TestGetBoardGroupsJobsIntoAllSixColumnsAcrossPages(t *testing.T) {
	// Arrange
	h := newJobsHarness(t)
	h.kagami.listJobs = func(in *kagamiv1.ListJobsRequest) (*kagamiv1.ListJobsResponse, error) {
		if in.GetPageToken() == "" {
			return &kagamiv1.ListJobsResponse{
				Jobs: []*kagamiv1.Job{
					kagamiJob("a", "Senior Backend Engineer", "Northwind", kagamiv1.JobStatus_JOB_STATUS_SAVED),
					kagamiJob("b", "Backend Engineer", "Lumen", kagamiv1.JobStatus_JOB_STATUS_APPLIED),
				},
				NextPageToken: "page-2",
			}, nil
		}
		return &kagamiv1.ListJobsResponse{Jobs: []*kagamiv1.Job{
			kagamiJob("c", "Go Developer", "Brightline", kagamiv1.JobStatus_JOB_STATUS_SAVED),
			kagamiJob("d", "Platform Engineer", "Halden Labs", kagamiv1.JobStatus_JOB_STATUS_INTERVIEW),
		}}, nil
	}

	// Act
	resp, err := h.client.GetBoard(context.Background(), withCookie(connect.NewRequest(&apiv1.GetBoardRequest{}), h.token))
	// Assert
	if err != nil {
		t.Fatalf("GetBoard: %v", err)
	}
	columns := resp.Msg.GetColumns()
	wantOrder := []apiv1.JobStatus{
		apiv1.JobStatus_JOB_STATUS_SAVED, apiv1.JobStatus_JOB_STATUS_APPLIED, apiv1.JobStatus_JOB_STATUS_SHORTLISTED,
		apiv1.JobStatus_JOB_STATUS_INTERVIEW, apiv1.JobStatus_JOB_STATUS_OFFER, apiv1.JobStatus_JOB_STATUS_REJECTED,
	}
	wantCounts := []int{2, 1, 0, 1, 0, 0}
	if len(columns) != len(wantOrder) {
		t.Fatalf("got %d columns, want %d", len(columns), len(wantOrder))
	}
	for i, column := range columns {
		if column.GetStatus() != wantOrder[i] || len(column.GetJobs()) != wantCounts[i] {
			t.Fatalf("column %d = %v with %d jobs, want %v with %d", i, column.GetStatus(), len(column.GetJobs()), wantOrder[i], wantCounts[i])
		}
	}
	saved := columns[0].GetJobs()
	if saved[0].GetId() != "a" || saved[1].GetId() != "c" || saved[0].GetCompanyName() != "Northwind" ||
		saved[0].GetNextFollowUp() != "2026-10-08" || saved[0].GetVersion() != 3 {
		t.Fatalf("saved column = %v", saved)
	}
	if owners := h.kagami.ownersSeen(); !reflect.DeepEqual(owners, []string{h.owner, h.owner}) {
		t.Fatalf("kagami calls acted for %v, want the signed-in owner twice", owners)
	}
}

func TestGetJobReturnsTheJobWithANewestFirstTimeline(t *testing.T) {
	// Arrange
	h := newJobsHarness(t)
	day := func(d int) *timestamppb.Timestamp {
		return timestamppb.New(time.Date(2026, 9, d, 10, 0, 0, 0, time.UTC))
	}
	h.kagami.getJob = func(*kagamiv1.GetJobRequest) (*kagamiv1.GetJobResponse, error) {
		return &kagamiv1.GetJobResponse{
			Job:     kagamiJob("job-1", "Platform Engineer", "Halden Labs", kagamiv1.JobStatus_JOB_STATUS_INTERVIEW),
			Company: &kagamiv1.Company{Name: "Halden Labs", Domain: "halden.example"},
			Events: []*kagamiv1.JobEvent{
				{Id: "e1", Kind: "status_changed", ToStatus: kagamiv1.JobStatus_JOB_STATUS_SAVED, OccurredAt: day(20)},
				{Id: "e3", Kind: "status_changed", FromStatus: kagamiv1.JobStatus_JOB_STATUS_APPLIED, ToStatus: kagamiv1.JobStatus_JOB_STATUS_INTERVIEW, OccurredAt: day(30)},
				{Id: "e2", Kind: "status_changed", FromStatus: kagamiv1.JobStatus_JOB_STATUS_SAVED, ToStatus: kagamiv1.JobStatus_JOB_STATUS_APPLIED, OccurredAt: day(23)},
			},
		}, nil
	}

	// Act
	resp, err := h.client.GetJob(context.Background(), withCookie(connect.NewRequest(&apiv1.GetJobRequest{Id: "job-1"}), h.token))
	// Assert
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}
	job := resp.Msg.GetJob()
	if job.GetCompanyDomain() != "halden.example" || job.GetStatus() != apiv1.JobStatus_JOB_STATUS_INTERVIEW {
		t.Fatalf("job = %v", job)
	}
	var ids []string
	for _, entry := range resp.Msg.GetTimeline() {
		ids = append(ids, entry.GetId())
	}
	if !reflect.DeepEqual(ids, []string{"e3", "e2", "e1"}) {
		t.Fatalf("timeline order = %v, want newest first [e3 e2 e1]", ids)
	}
	if first := resp.Msg.GetTimeline()[0]; first.GetFromStatus() != apiv1.JobStatus_JOB_STATUS_APPLIED || first.GetToStatus() != apiv1.JobStatus_JOB_STATUS_INTERVIEW {
		t.Fatalf("first entry statuses = %v -> %v", first.GetFromStatus(), first.GetToStatus())
	}
}

func TestAddJobForwardsTheFieldsAndTheIdempotencyKey(t *testing.T) {
	// Arrange
	h := newJobsHarness(t)
	h.kagami.addJob = func(in *kagamiv1.AddJobRequest) (*kagamiv1.AddJobResponse, error) {
		return &kagamiv1.AddJobResponse{
			Job:     kagamiJob("job-9", in.GetTitle(), in.GetCompanyName(), kagamiv1.JobStatus_JOB_STATUS_SAVED),
			Company: &kagamiv1.Company{Domain: in.GetCompanyDomain()},
		}, nil
	}
	req := withCookie(connect.NewRequest(&apiv1.AddJobRequest{
		Title: "SRE", CompanyName: "Tessellate", CompanyDomain: "tessellate.example", Url: "https://tessellate.example/jobs/1",
		Status: apiv1.JobStatus_JOB_STATUS_APPLIED, Location: "Remote",
	}), h.token)
	req.Header().Set("Idempotency-Key", "key-1")

	// Act
	resp, err := h.client.AddJob(context.Background(), req)
	// Assert
	if err != nil {
		t.Fatalf("AddJob: %v", err)
	}
	if got := resp.Msg.GetJob(); got.GetId() != "job-9" || got.GetCompanyDomain() != "tessellate.example" {
		t.Fatalf("job = %v", got)
	}
	sent, ok := h.kagami.requestsSeen()[0].(*kagamiv1.AddJobRequest)
	if !ok || sent.GetIdempotencyKey() != "key-1" || sent.GetStatus() != kagamiv1.JobStatus_JOB_STATUS_APPLIED ||
		sent.GetLocation() != "Remote" || sent.GetUrl() != "https://tessellate.example/jobs/1" {
		t.Fatalf("kagami request = %v", h.kagami.requestsSeen()[0])
	}
}

func TestUpdateJobOnlyAllowsTheEditableFields(t *testing.T) {
	tests := []struct {
		name     string
		paths    []string
		wantCode connect.Code
	}{
		{"no mask", nil, connect.CodeInvalidArgument},
		{"status is not editable here", []string{"status"}, connect.CodeInvalidArgument},
		{"company is not editable", []string{"company_name"}, connect.CodeInvalidArgument},
		{"one bad field spoils the mask", []string{"next_follow_up", "version"}, connect.CodeInvalidArgument},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			h := newJobsHarness(t)
			req := withCookie(connect.NewRequest(&apiv1.UpdateJobRequest{
				Job: &apiv1.Job{Id: "job-1"}, UpdateMask: &fieldmaskpb.FieldMask{Paths: tt.paths}, Version: 3,
			}), h.token)

			// Act
			_, err := h.client.UpdateJob(context.Background(), req)

			// Assert
			if connect.CodeOf(err) != tt.wantCode || reasonOf(err) != "UPDATE_MASK_INVALID" {
				t.Fatalf("code %v reason %q, want %v UPDATE_MASK_INVALID", connect.CodeOf(err), reasonOf(err), tt.wantCode)
			}
			if len(h.kagami.requestsSeen()) != 0 {
				t.Fatal("kagami was called for a rejected mask")
			}
		})
	}
}

func TestUpdateJobForwardsTheFollowUpDateAndVersion(t *testing.T) {
	// Arrange
	h := newJobsHarness(t)
	h.kagami.updateJob = func(in *kagamiv1.UpdateJobRequest) (*kagamiv1.UpdateJobResponse, error) {
		job := kagamiJob(in.GetJob().GetId(), "Platform Engineer", "Halden Labs", kagamiv1.JobStatus_JOB_STATUS_INTERVIEW)
		job.NextFollowUp = in.GetJob().GetNextFollowUp()
		job.Version = in.GetVersion() + 1
		return &kagamiv1.UpdateJobResponse{Job: job}, nil
	}
	req := withCookie(connect.NewRequest(&apiv1.UpdateJobRequest{
		Job:        &apiv1.Job{Id: "job-1", NextFollowUp: "2026-10-12"},
		UpdateMask: &fieldmaskpb.FieldMask{Paths: []string{"next_follow_up"}},
		Version:    3,
	}), h.token)

	// Act
	resp, err := h.client.UpdateJob(context.Background(), req)
	// Assert
	if err != nil {
		t.Fatalf("UpdateJob: %v", err)
	}
	if got := resp.Msg.GetJob(); got.GetNextFollowUp() != "2026-10-12" || got.GetVersion() != 4 {
		t.Fatalf("job = %v", got)
	}
}

func TestChangeJobStatusKeepsTheInvalidTransitionReason(t *testing.T) {
	// Arrange
	h := newJobsHarness(t)
	h.kagami.changeJobStatus = func(*kagamiv1.ChangeJobStatusRequest) (*kagamiv1.ChangeJobStatusResponse, error) {
		return nil, grpcError(codes.FailedPrecondition, "JOB_STATUS_INVALID_TRANSITION", "cannot move from saved to offer")
	}

	// Act
	_, err := h.client.ChangeJobStatus(context.Background(), withCookie(connect.NewRequest(&apiv1.ChangeJobStatusRequest{
		Id: "job-1", ToStatus: apiv1.JobStatus_JOB_STATUS_OFFER, Version: 3,
	}), h.token))

	// Assert
	if connect.CodeOf(err) != connect.CodeFailedPrecondition || reasonOf(err) != "JOB_STATUS_INVALID_TRANSITION" {
		t.Fatalf("code %v reason %q, want FailedPrecondition JOB_STATUS_INVALID_TRANSITION", connect.CodeOf(err), reasonOf(err))
	}
}

func TestChangeJobStatusKeepsAStaleVersionConflict(t *testing.T) {
	// Arrange
	h := newJobsHarness(t)
	h.kagami.changeJobStatus = func(*kagamiv1.ChangeJobStatusRequest) (*kagamiv1.ChangeJobStatusResponse, error) {
		return nil, grpcError(codes.Aborted, "VERSION_CONFLICT", "the job changed since you loaded it")
	}

	// Act
	_, err := h.client.ChangeJobStatus(context.Background(), withCookie(connect.NewRequest(&apiv1.ChangeJobStatusRequest{
		Id: "job-1", ToStatus: apiv1.JobStatus_JOB_STATUS_APPLIED, Version: 1,
	}), h.token))

	// Assert
	if connect.CodeOf(err) != connect.CodeAborted || reasonOf(err) != "VERSION_CONFLICT" {
		t.Fatalf("code %v reason %q, want Aborted VERSION_CONFLICT", connect.CodeOf(err), reasonOf(err))
	}
}

func TestDownstreamFailuresAreNotLeakedToTheBrowser(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode connect.Code
		wantSafe string
	}{
		{"internal error", status.Error(codes.Internal, "pq: password authentication failed for user kagami"), connect.CodeInternal, "something went wrong"},
		{"torii's own identity refused", status.Error(codes.Unauthenticated, "bad signature on identity token"), connect.CodeInternal, "something went wrong"},
		{"downstream down", status.Error(codes.Unavailable, "dial tcp 10.0.3.7:9090: connection refused"), connect.CodeUnavailable, "the service is busy, try again"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			h := newJobsHarness(t)
			h.kagami.getJob = func(*kagamiv1.GetJobRequest) (*kagamiv1.GetJobResponse, error) { return nil, tt.err }

			// Act
			_, err := h.client.GetJob(context.Background(), withCookie(connect.NewRequest(&apiv1.GetJobRequest{Id: "job-1"}), h.token))

			// Assert
			if connect.CodeOf(err) != tt.wantCode {
				t.Fatalf("code = %v, want %v", connect.CodeOf(err), tt.wantCode)
			}
			if !strings.Contains(err.Error(), tt.wantSafe) {
				t.Fatalf("error = %q, want the safe message %q", err.Error(), tt.wantSafe)
			}
			for _, leaked := range []string{"pq:", "password", "bad signature", "10.0.3.7"} {
				if strings.Contains(err.Error(), leaked) {
					t.Fatalf("error %q leaks %q", err.Error(), leaked)
				}
			}
		})
	}
}

func TestJobsRequireASession(t *testing.T) {
	// Arrange
	h := newJobsHarness(t)

	// Act
	_, err := h.client.GetBoard(context.Background(), connect.NewRequest(&apiv1.GetBoardRequest{}))

	// Assert
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", connect.CodeOf(err))
	}
	if len(h.kagami.requestsSeen()) != 0 {
		t.Fatal("kagami was called without a session")
	}
}
