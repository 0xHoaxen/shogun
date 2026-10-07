package app

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/services/shinobi/internal/store"
)

const (
	// trackerSource is the source kagami records for a job found here.
	trackerSource = "discovery"
	// unknownCompany names the company of a posting that does not say.
	unknownCompany = "Unknown company"
	// maxTrackedDescription caps the description passed to the tracker.
	maxTrackedDescription = 5000
)

// Errors the transport maps to gRPC status codes.
var (
	// ErrPostingNotFound means the owner has no such posting.
	ErrPostingNotFound = errors.New("app: posting not found")
	// ErrTrackerUnavailable means kagami could not be reached or failed.
	ErrTrackerUnavailable = errors.New("app: the tracker is unavailable")
	// ErrPostingRefused means kagami refused the posting as a job.
	ErrPostingRefused = errors.New("app: the tracker refused the posting")
)

// Tracker is the part of kagami's client that SaveToTracker uses.
type Tracker interface {
	AddJob(ctx context.Context, in *kagamiv1.AddJobRequest, opts ...grpc.CallOption) (*kagamiv1.AddJobResponse, error)
}

// WithTracker sets the tracker postings are saved to.
func WithTracker(t Tracker) Option { return func(s *Service) { s.tracker = t } }

// SaveToTracker adds a posting to kagami as a saved job and remembers which job
// it became. Saving again returns the same job: kagami is asked with the
// posting's id as the idempotency key, so even a call that failed after the job
// was made gives the original back.
func (s *Service) SaveToTracker(ctx context.Context, postingID uuid.UUID) (uuid.UUID, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	if s.tracker == nil {
		return uuid.Nil, ErrTrackerUnavailable
	}
	repo := store.New(s.pool)
	posting, err := repo.GetPosting(ctx, owner, postingID)
	if errors.Is(err, store.ErrNotFound) {
		return uuid.Nil, ErrPostingNotFound
	}
	if err != nil {
		return uuid.Nil, err
	}
	if posting.SavedJobID != nil {
		return *posting.SavedJobID, nil
	}

	c := candidateOf(posting)
	res, err := s.tracker.AddJob(ctx, &kagamiv1.AddJobRequest{
		Title: c.Title, CompanyName: companyOf(c.Company, c.URL), Url: c.URL, Source: trackerSource,
		Status: kagamiv1.JobStatus_JOB_STATUS_SAVED, Location: c.Location,
		Description: clip(c.Description, maxTrackedDescription), IdempotencyKey: posting.ID.String(),
	})
	if err != nil {
		return uuid.Nil, trackerError(err)
	}
	jobID, err := uuid.Parse(res.GetJob().GetId())
	if err != nil {
		return uuid.Nil, fmt.Errorf("%w: the tracker returned no job id", ErrTrackerUnavailable)
	}
	saved, _, err := repo.SetSavedJob(ctx, owner, postingID, jobID)
	if err != nil {
		return uuid.Nil, wrap("remember the saved job", err)
	}
	// Another call may have recorded its job first; the first one stands.
	return *saved.SavedJobID, nil
}

// companyOf names the company of a posting: what it says, else the host of its
// link, else a placeholder.
func companyOf(company, link string) string {
	if company = strings.TrimSpace(company); company != "" {
		return company
	}
	if u, err := url.Parse(link); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return unknownCompany
}

// trackerError separates a posting kagami refused from kagami being unreachable.
func trackerError(err error) error {
	switch status.Code(err) { //nolint:exhaustive // every other code means the tracker is not available
	case codes.InvalidArgument, codes.FailedPrecondition, codes.AlreadyExists, codes.OutOfRange:
		return fmt.Errorf("%w: %s", ErrPostingRefused, status.Code(err))
	default:
		return fmt.Errorf("%w: %s", ErrTrackerUnavailable, status.Code(err))
	}
}
