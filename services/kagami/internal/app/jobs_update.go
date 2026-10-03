package app

import (
	"context"
	"fmt"

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
		from := domain.JobStatus(current.Status)
		moved, err := domain.Job{Status: from}.ChangeStatus(in.To, s.now().UTC())
		if err != nil {
			return err
		}

		appliedOn := current.AppliedOn
		if moved.Status == domain.JobApplied && appliedOn == nil {
			day := s.today()
			appliedOn = &day
		}
		updated, err = repo.UpdateJobStatus(ctx, db.UpdateJobStatusParams{
			ID: id, OwnerID: owner, Version: in.Version, Status: string(moved.Status), AppliedOn: appliedOn,
		})
		if err != nil {
			return err
		}

		fromText, toText := string(from), string(moved.Status)
		payload := map[string]any{}
		if in.Note != "" {
			payload["note"] = in.Note
		}
		if err := s.addJobEvent(ctx, repo, id, jobEventStatus, &fromText, &toText, payload); err != nil {
			return err
		}
		_, err = outbox.Write(ctx, tx, eventSource, eventJobStatusChanged, id.String(), &kagamiv1.JobStatusChanged{
			JobId: id.String(),
			From:  wire.JobStatusToProto(from),
			To:    wire.JobStatusToProto(moved.Status),
			At:    timestamppb.New(s.now().UTC()),
		})
		if err != nil {
			return fmt.Errorf("write %s event: %w", eventJobStatusChanged, err)
		}
		return nil
	})
	return updated, err
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
