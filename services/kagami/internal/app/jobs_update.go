package app

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/pkg/outbox"
	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store/db"
	"github.com/0xHoaxen/shogun/services/kagami/internal/wire"
)

// UpdateJobInput is the input of UpdateJob. Paths are the proto field names
// named in the update mask; only those fields of Fields are applied.
type UpdateJobInput struct {
	ID      string
	Version int32
	Paths   []string
	Fields  JobFields
}

// ChangeJobStatusInput is the input of ChangeJobStatus.
type ChangeJobStatusInput struct {
	ID      string
	To      domain.JobStatus
	Note    string
	Version int32
}

// ChangeJobStatus moves a job along the job state machine. It writes a
// status_changed timeline entry and the job.status_changed event with the
// update.
func (s *Service) ChangeJobStatus(ctx context.Context, in ChangeJobStatusInput) (db.Job, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return db.Job{}, err
	}
	id, err := parseID("id", in.ID)
	if err != nil {
		return db.Job{}, err
	}
	if in.Version < 1 {
		return db.Job{}, invalid("VERSION_REQUIRED", "version is required")
	}
	if !in.To.Valid() {
		return db.Job{}, invalid("INVALID_STATUS", "unknown status %q", in.To)
	}

	var updated db.Job
	err = s.inTx(ctx, func(tx pgx.Tx, repo *store.Repo) error {
		current, err := repo.GetJob(ctx, owner, id)
		if err != nil {
			return err
		}
		if current.Version != in.Version {
			return store.ErrVersionConflict
		}
		updated, err = s.moveJob(ctx, tx, repo, owner, current, in.To, in.Note, nil)
		return err
	})
	return updated, err
}

// moveJob moves a job to a new status inside tx: the row (with its optimistic
// version, which must be the one in current), a timeline entry and a
// job.status_changed event. note and extra go to the timeline entry's payload.
// An invalid move is a *domain.TransitionError. The user's ChangeJobStatus and
// the mail event handler share it.
func (s *Service) moveJob(
	ctx context.Context, tx pgx.Tx, repo *store.Repo, owner uuid.UUID, current db.Job,
	to domain.JobStatus, note string, extra map[string]any,
) (db.Job, error) {
	from := domain.JobStatus(current.Status)
	moved, err := domain.Job{Status: from}.ChangeStatus(to, s.now().UTC())
	if err != nil {
		return db.Job{}, err
	}

	appliedOn := current.AppliedOn
	if moved.Status == domain.JobApplied && appliedOn == nil {
		day := s.today()
		appliedOn = &day
	}
	updated, err := repo.UpdateJobStatus(ctx, db.UpdateJobStatusParams{
		ID: current.ID, OwnerID: owner, Version: current.Version, Status: string(moved.Status), AppliedOn: appliedOn,
	})
	if err != nil {
		return db.Job{}, err
	}

	fromText, toText := string(from), string(moved.Status)
	payload := map[string]any{}
	for k, v := range extra {
		payload[k] = v
	}
	if note != "" {
		payload["note"] = note
	}
	if err := s.addJobEvent(ctx, repo, current.ID, jobEventStatus, &fromText, &toText, payload); err != nil {
		return db.Job{}, err
	}
	_, err = outbox.Write(ctx, tx, eventSource, eventJobStatusChanged, current.ID.String(), &kagamiv1.JobStatusChanged{
		JobId: current.ID.String(),
		From:  wire.JobStatusToProto(from),
		To:    wire.JobStatusToProto(moved.Status),
		At:    timestamppb.New(s.now().UTC()),
	})
	if err != nil {
		return db.Job{}, fmt.Errorf("write %s event: %w", eventJobStatusChanged, err)
	}
	return updated, nil
}

// UpdateJob applies the masked fields to a job when the version is current.
// It writes no outbox event: no consumer needs one for plain edits. A changed
// next_follow_up adds a follow_up_set timeline entry.
func (s *Service) UpdateJob(ctx context.Context, in UpdateJobInput) (db.Job, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return db.Job{}, err
	}
	id, err := parseID("id", in.ID)
	if err != nil {
		return db.Job{}, err
	}
	if in.Version < 1 {
		return db.Job{}, invalid("VERSION_REQUIRED", "version is required")
	}

	var updated db.Job
	err = s.inTx(ctx, func(_ pgx.Tx, repo *store.Repo) error {
		current, err := repo.GetJob(ctx, owner, id)
		if err != nil {
			return err
		}
		if current.Version != in.Version {
			return store.ErrVersionConflict
		}
		masked, err := applyJobMask(current, in.Paths, in.Fields)
		if err != nil {
			return err
		}
		updated, err = repo.UpdateJob(ctx, masked.params)
		if err != nil {
			return err
		}
		if !masked.followUpChanged {
			return nil
		}
		var due any
		if updated.NextFollowUp != nil {
			due = updated.NextFollowUp.Format(dateLayout)
		}
		return s.addJobEvent(ctx, repo, id, jobEventFollowUpSet, nil, nil, map[string]any{"next_follow_up": due})
	})
	return updated, err
}
