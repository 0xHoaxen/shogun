package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/pkg/outbox"
	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store/db"
)

// Event types and job_events kinds written by the job use cases.
const (
	eventJobAdded         = "job.added"
	eventJobStatusChanged = "job.status_changed"

	jobEventCreated     = "created"
	jobEventStatus      = "status_changed"
	jobEventFollowUpSet = "follow_up_set"

	defaultJobSource     = "manual"
	maxIdempotencyKeyLen = 200
)

// AddJobInput is the input of AddJob.
type AddJobInput struct {
	Title       string
	CompanyName string
	// CompanyDomain is the upsert key for the company; optional.
	CompanyDomain string
	URL           string
	Source        string
	// Status defaults to saved.
	Status         domain.JobStatus
	Location       string
	SalaryText     string
	Description    string
	IdempotencyKey string
}

// JobResult is a job with its company.
type JobResult struct {
	Job     db.Job
	Company db.Company
}

// JobDetail is a job with its company and timeline, newest event first.
type JobDetail struct {
	JobResult
	Events []db.JobEvent
}

// ListJobsInput is the input of ListJobs. Zero values do not filter.
type ListJobsInput struct {
	Status    domain.JobStatus
	CompanyID string
	// DueBefore is a YYYY-MM-DD date.
	DueBefore string
	PageSize  int32
	PageToken string
}

// ListJobsResult is one page of jobs.
type ListJobsResult struct {
	Jobs          []db.Job
	NextPageToken string
}

// AddJob creates a job and upserts its company by domain, or by name when
// there is no domain. A repeated idempotency key returns the original job and
// writes nothing.
func (s *Service) AddJob(ctx context.Context, in AddJobInput) (JobResult, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return JobResult{}, err
	}
	in, err = normalizeAddJob(in)
	if err != nil {
		return JobResult{}, err
	}

	var res JobResult
	err = s.inTx(ctx, func(tx pgx.Tx, repo *store.Repo) error {
		replay, found, err := replayJob(ctx, repo, owner, in.IdempotencyKey)
		if err != nil || found {
			res = replay
			return err
		}
		res, err = s.createJob(ctx, tx, repo, owner, in)
		return err
	})
	return res, err
}

func normalizeAddJob(in AddJobInput) (AddJobInput, error) {
	in.Title = strings.TrimSpace(in.Title)
	in.CompanyName = strings.TrimSpace(in.CompanyName)
	in.CompanyDomain = strings.ToLower(strings.TrimSpace(in.CompanyDomain))
	if in.Title == "" {
		return in, invalid("TITLE_REQUIRED", "title is required")
	}
	if in.CompanyName == "" {
		return in, invalid("COMPANY_REQUIRED", "company name is required")
	}
	if in.Source == "" {
		in.Source = defaultJobSource
	}
	if !slices.Contains(jobSources, in.Source) {
		return in, invalid("INVALID_SOURCE", "source must be one of %v", jobSources)
	}
	if in.Status == "" {
		in.Status = domain.JobSaved
	}
	if !in.Status.Valid() {
		return in, invalid("INVALID_STATUS", "unknown status %q", in.Status)
	}
	if len(in.IdempotencyKey) > maxIdempotencyKeyLen {
		return in, invalid("INVALID_IDEMPOTENCY_KEY", "idempotency key is longer than %d bytes", maxIdempotencyKeyLen)
	}
	return in, nil
}

// replayJob returns the job an earlier call with the same key created.
func replayJob(ctx context.Context, repo *store.Repo, owner uuid.UUID, key string) (JobResult, bool, error) {
	if key == "" {
		return JobResult{}, false, nil
	}
	job, err := repo.GetJobByIdempotencyKey(ctx, owner, key)
	if errors.Is(err, store.ErrNotFound) {
		return JobResult{}, false, nil
	}
	if err != nil {
		return JobResult{}, false, fmt.Errorf("find job by idempotency key: %w", err)
	}
	company, err := repo.GetCompany(ctx, owner, job.CompanyID)
	if err != nil {
		return JobResult{}, false, fmt.Errorf("load company of job: %w", err)
	}
	return JobResult{Job: job, Company: company}, true, nil
}

func (s *Service) createJob(ctx context.Context, tx pgx.Tx, repo *store.Repo, owner uuid.UUID, in AddJobInput) (JobResult, error) {
	var company db.Company
	var err error
	if in.CompanyDomain != "" {
		company, err = repo.UpsertCompanyByDomain(ctx, owner, in.CompanyName, in.CompanyDomain)
	} else {
		company, err = repo.FindOrCreateCompanyByName(ctx, owner, in.CompanyName)
	}
	if err != nil {
		return JobResult{}, err
	}

	var appliedOn *time.Time
	if in.Status == domain.JobApplied {
		day := s.today()
		appliedOn = &day
	}
	job, err := repo.InsertJob(ctx, db.InsertJobParams{
		ID: store.NewID(), OwnerID: owner, CompanyID: company.ID, Title: in.Title,
		Url: strPtr(in.URL), Source: in.Source, Status: string(in.Status), AppliedOn: appliedOn,
		Location: strPtr(in.Location), SalaryText: strPtr(in.SalaryText),
		Description: strPtr(in.Description), IdempotencyKey: strPtr(in.IdempotencyKey),
	})
	if err != nil {
		return JobResult{}, err
	}
	if err := s.addJobEvent(ctx, repo, job.ID, jobEventCreated, nil, &job.Status, nil); err != nil {
		return JobResult{}, err
	}
	_, err = outbox.Write(ctx, tx, eventSource, eventJobAdded, job.ID.String(), &kagamiv1.JobAdded{
		JobId: job.ID.String(), Title: job.Title, Company: company.Name, Url: in.URL, Source: job.Source,
	})
	if err != nil {
		return JobResult{}, fmt.Errorf("write %s event: %w", eventJobAdded, err)
	}
	return JobResult{Job: job, Company: company}, nil
}

// addJobEvent appends one entry to a job's timeline.
func (s *Service) addJobEvent(ctx context.Context, repo *store.Repo, jobID uuid.UUID, kind string, from, to *string, payload map[string]any) error {
	if payload == nil {
		payload = map[string]any{}
	}
	_, err := repo.InsertJobEvent(ctx, db.InsertJobEventParams{
		ID: store.NewID(), JobID: jobID, Kind: kind, FromStatus: from, ToStatus: to,
		Payload: jsonObject(payload), OccurredAt: s.now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("add %s job event: %w", kind, err)
	}
	return nil
}

// GetJob returns a job with its company and timeline.
func (s *Service) GetJob(ctx context.Context, id string) (JobDetail, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return JobDetail{}, err
	}
	jobID, err := parseID("id", id)
	if err != nil {
		return JobDetail{}, err
	}
	repo := store.New(s.pool)
	job, err := repo.GetJob(ctx, owner, jobID)
	if err != nil {
		return JobDetail{}, err
	}
	company, err := repo.GetCompany(ctx, owner, job.CompanyID)
	if err != nil {
		return JobDetail{}, fmt.Errorf("load company of job: %w", err)
	}
	events, err := repo.ListJobEvents(ctx, job.ID)
	if err != nil {
		return JobDetail{}, err
	}
	return JobDetail{JobResult: JobResult{Job: job, Company: company}, Events: events}, nil
}

// ListJobs returns one page of jobs, most recently updated first.
func (s *Service) ListJobs(ctx context.Context, in ListJobsInput) (ListJobsResult, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return ListJobsResult{}, err
	}
	filter, err := jobFilter(in)
	if err != nil {
		return ListJobsResult{}, err
	}
	jobs, next, err := store.New(s.pool).ListJobs(ctx, owner, filter, store.Page{Size: in.PageSize, Token: in.PageToken})
	if err != nil {
		return ListJobsResult{}, err
	}
	return ListJobsResult{Jobs: jobs, NextPageToken: next}, nil
}

func jobFilter(in ListJobsInput) (store.JobFilter, error) {
	var f store.JobFilter
	if in.Status != "" {
		if !in.Status.Valid() {
			return f, invalid("INVALID_STATUS", "unknown status %q", in.Status)
		}
		status := string(in.Status)
		f.Status = &status
	}
	if in.CompanyID != "" {
		id, err := parseID("company_id", in.CompanyID)
		if err != nil {
			return f, err
		}
		f.CompanyID = &id
	}
	due, err := parseDate("due_before", in.DueBefore)
	if err != nil {
		return f, err
	}
	f.DueBefore = due
	return f, nil
}
