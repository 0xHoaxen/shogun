package store

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/kagami/internal/store/db"
)

// JobFilter narrows ListJobs. A nil field does not filter.
type JobFilter struct {
	Status    *string
	CompanyID *uuid.UUID
	// DueBefore keeps jobs whose next_follow_up is on or before this date.
	DueBefore *time.Time
}

// InsertJob creates a job. A repeated url or idempotency key is ErrDuplicate.
func (r *Repo) InsertJob(ctx context.Context, arg db.InsertJobParams) (db.Job, error) {
	j, err := r.q.InsertJob(ctx, arg)
	if err != nil {
		return db.Job{}, fmt.Errorf("insert job: %w", mapErr(err))
	}
	return j, nil
}

// GetJob returns one job of the owner.
func (r *Repo) GetJob(ctx context.Context, owner, id uuid.UUID) (db.Job, error) {
	j, err := r.q.GetJob(ctx, db.GetJobParams{ID: id, OwnerID: owner})
	return j, mapErr(err)
}

// GetJobByURL returns the owner's job posted at url, as the CSV job_link
// column names it.
func (r *Repo) GetJobByURL(ctx context.Context, owner uuid.UUID, url string) (db.Job, error) {
	j, err := r.q.GetJobByURL(ctx, db.GetJobByURLParams{OwnerID: owner, Url: url})
	return j, mapErr(err)
}

// GetJobByIdempotencyKey returns the job a retried AddJob created earlier.
func (r *Repo) GetJobByIdempotencyKey(ctx context.Context, owner uuid.UUID, key string) (db.Job, error) {
	j, err := r.q.GetJobByIdempotencyKey(ctx, db.GetJobByIdempotencyKeyParams{OwnerID: owner, IdempotencyKey: key})
	return j, mapErr(err)
}

// ListJobs returns one page of the owner's jobs, most recently updated first,
// and the token for the next page, empty on the last page.
func (r *Repo) ListJobs(ctx context.Context, owner uuid.UUID, f JobFilter, p Page) ([]db.Job, string, error) {
	after, err := decodeCursor(p.Token)
	if err != nil {
		return nil, "", err
	}
	size := p.size()
	afterAt, afterID := afterParams(after)
	rows, err := r.q.ListJobs(ctx, db.ListJobsParams{
		OwnerID: owner, Status: f.Status, CompanyID: f.CompanyID, DueBefore: f.DueBefore,
		AfterUpdatedAt: afterAt, AfterID: afterID, RowLimit: size + 1,
	})
	if err != nil {
		return nil, "", fmt.Errorf("list jobs: %w", mapErr(err))
	}
	rows, next := trimPage(rows, size, func(j db.Job) (time.Time, uuid.UUID) { return j.UpdatedAt, j.ID })
	return rows, next, nil
}

// UpdateJob writes the editable columns when arg.Version is current. A stale
// version is ErrVersionConflict.
func (r *Repo) UpdateJob(ctx context.Context, arg db.UpdateJobParams) (db.Job, error) {
	j, err := r.q.UpdateJob(ctx, arg)
	if err != nil {
		return db.Job{}, staleOrMissing(err, func() error {
			_, getErr := r.q.GetJob(ctx, db.GetJobParams{ID: arg.ID, OwnerID: arg.OwnerID})
			return getErr
		})
	}
	return j, nil
}

// UpdateJobStatus writes the status when arg.Version is current. A stale
// version is ErrVersionConflict.
func (r *Repo) UpdateJobStatus(ctx context.Context, arg db.UpdateJobStatusParams) (db.Job, error) {
	j, err := r.q.UpdateJobStatus(ctx, arg)
	if err != nil {
		return db.Job{}, staleOrMissing(err, func() error {
			_, getErr := r.q.GetJob(ctx, db.GetJobParams{ID: arg.ID, OwnerID: arg.OwnerID})
			return getErr
		})
	}
	return j, nil
}

// ListJobsDueOn returns every owner's open jobs whose follow-up is on date.
func (r *Repo) ListJobsDueOn(ctx context.Context, date time.Time) ([]db.Job, error) {
	jobs, err := r.q.ListJobsDueOn(ctx, date)
	if err != nil {
		return nil, fmt.Errorf("list jobs due on %s: %w", date.Format(time.DateOnly), mapErr(err))
	}
	return jobs, nil
}

// ListStaleAppliedJobs returns applied jobs with no follow-up planned that
// were applied for on or before appliedBy.
func (r *Repo) ListStaleAppliedJobs(ctx context.Context, appliedBy time.Time) ([]db.Job, error) {
	jobs, err := r.q.ListStaleAppliedJobs(ctx, appliedBy)
	if err != nil {
		return nil, fmt.Errorf("list stale applied jobs: %w", mapErr(err))
	}
	return jobs, nil
}

// MarkJobFollowUp plans a follow-up for a job that has none. It returns
// ErrNotFound when the job is gone or already has one.
func (r *Repo) MarkJobFollowUp(ctx context.Context, id uuid.UUID, date time.Time) (db.Job, error) {
	j, err := r.q.MarkJobFollowUp(ctx, db.MarkJobFollowUpParams{ID: id, NextFollowUp: date})
	return j, mapErr(err)
}

// ListDueJobs returns up to limit of the owner's open jobs with a follow-up on
// or before date, the oldest first.
func (r *Repo) ListDueJobs(ctx context.Context, owner uuid.UUID, date time.Time, limit int32) ([]db.Job, error) {
	jobs, err := r.q.ListDueJobs(ctx, db.ListDueJobsParams{OwnerID: owner, OnOrBefore: date, RowLimit: limit})
	if err != nil {
		return nil, fmt.Errorf("list due jobs: %w", mapErr(err))
	}
	return jobs, nil
}

// InsertJobEvent appends to a job's timeline.
func (r *Repo) InsertJobEvent(ctx context.Context, arg db.InsertJobEventParams) (db.JobEvent, error) {
	e, err := r.q.InsertJobEvent(ctx, arg)
	if err != nil {
		return db.JobEvent{}, fmt.Errorf("insert job event: %w", mapErr(err))
	}
	return e, nil
}

// ListJobEvents returns a job's timeline, newest first.
func (r *Repo) ListJobEvents(ctx context.Context, jobID uuid.UUID) ([]db.JobEvent, error) {
	events, err := r.q.ListJobEvents(ctx, jobID)
	if err != nil {
		return nil, fmt.Errorf("list job events: %w", mapErr(err))
	}
	return events, nil
}

// FindJobByURLs returns the owner's job posted at one of urls, the newest when
// several. It is ErrNotFound when none is.
func (r *Repo) FindJobByURLs(ctx context.Context, owner uuid.UUID, urls []string) (db.Job, error) {
	j, err := r.q.FindJobByURLs(ctx, db.FindJobByURLsParams{OwnerID: owner, Urls: urls})
	return j, mapErr(err)
}

// FindJobByCompanyDomains returns a job at a company with one of the domains,
// preferring an open job and the most specific domain. It is ErrNotFound when
// none is.
func (r *Repo) FindJobByCompanyDomains(ctx context.Context, owner uuid.UUID, domains []string) (db.Job, error) {
	j, err := r.q.FindJobByCompanyDomains(ctx, db.FindJobByCompanyDomainsParams{OwnerID: owner, Domains: domains})
	return j, mapErr(err)
}
