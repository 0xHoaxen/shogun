package app

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/pkg/outbox"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store"
	"github.com/0xHoaxen/shogun/services/kagami/internal/store/db"
)

const (
	eventJobFollowUpDue     = "job.follow_up_due"
	eventContactFollowUpDue = "contact.follow_up_due"

	// staleAfterDays is how long an application may sit without a planned
	// follow-up before the stale scan plans one. TODO(owner): confirm 7 days.
	staleAfterDays = 7
	// maxDueItems caps each list in DueFollowUps.
	maxDueItems = 500
)

// DueFollowUps are the jobs and contacts to follow up on.
type DueFollowUps struct {
	Jobs     []db.Job
	Contacts []db.Contact
}

// DueFollowUps returns the owner's jobs and contacts whose next_follow_up is
// on or before onOrBefore (YYYY-MM-DD), the oldest first. Empty means today.
func (s *Service) DueFollowUps(ctx context.Context, onOrBefore string) (DueFollowUps, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return DueFollowUps{}, err
	}
	date := s.today()
	if onOrBefore != "" {
		parsed, err := parseDate("on_or_before", onOrBefore)
		if err != nil {
			return DueFollowUps{}, err
		}
		date = *parsed
	}
	repo := store.New(s.pool)
	jobs, err := repo.ListDueJobs(ctx, owner, date, maxDueItems)
	if err != nil {
		return DueFollowUps{}, err
	}
	contacts, err := repo.ListDueContacts(ctx, owner, date, maxDueItems)
	if err != nil {
		return DueFollowUps{}, err
	}
	return DueFollowUps{Jobs: jobs, Contacts: contacts}, nil
}

// ScanDueFollowUps emits job.follow_up_due and contact.follow_up_due for every
// owner's item whose follow-up is exactly on date, and returns how many it
// emitted. All events are written in one transaction, so a failed scan emits
// nothing and a retry cannot double up. Running it once per date is the
// caller's job; the River job's args carry the date to guarantee that.
func (s *Service) ScanDueFollowUps(ctx context.Context, date time.Time) (int, error) {
	dueOn := date.Format(dateLayout)
	emitted := 0
	err := s.inTx(ctx, func(tx pgx.Tx, repo *store.Repo) error {
		jobs, err := repo.ListJobsDueOn(ctx, date)
		if err != nil {
			return err
		}
		for _, job := range jobs {
			_, err := outbox.Write(ctx, tx, eventSource, eventJobFollowUpDue, job.ID.String(),
				&kagamiv1.JobFollowUpDue{JobId: job.ID.String(), DueOn: dueOn, OwnerId: job.OwnerID.String()})
			if err != nil {
				return fmt.Errorf("write %s event: %w", eventJobFollowUpDue, err)
			}
		}
		contacts, err := repo.ListContactsDueOn(ctx, date)
		if err != nil {
			return err
		}
		for _, contact := range contacts {
			_, err := outbox.Write(ctx, tx, eventSource, eventContactFollowUpDue, contact.ID.String(),
				&kagamiv1.ContactFollowUpDue{ContactId: contact.ID.String(), DueOn: dueOn, OwnerId: contact.OwnerID.String()})
			if err != nil {
				return fmt.Errorf("write %s event: %w", eventContactFollowUpDue, err)
			}
		}
		emitted = len(jobs) + len(contacts)
		return nil
	})
	return emitted, err
}

// ScanStaleApplications plans a follow-up on date for every applied job that
// has had none for staleAfterDays, and returns how many it planned. It writes
// a follow_up_set timeline entry but no outbox event: the due scan that runs
// after it emits job.follow_up_due for these jobs along with the others.
func (s *Service) ScanStaleApplications(ctx context.Context, date time.Time) (int, error) {
	appliedBy := date.AddDate(0, 0, -staleAfterDays)
	planned := 0
	err := s.inTx(ctx, func(_ pgx.Tx, repo *store.Repo) error {
		stale, err := repo.ListStaleAppliedJobs(ctx, appliedBy)
		if err != nil {
			return err
		}
		for _, job := range stale {
			if _, err := repo.MarkJobFollowUp(ctx, job.ID, date); err != nil {
				return err
			}
			err := s.addJobEvent(ctx, repo, job.ID, jobEventFollowUpSet, nil, nil, map[string]any{
				"next_follow_up": date.Format(dateLayout),
				"reason":         "stale_application",
			})
			if err != nil {
				return err
			}
		}
		planned = len(stale)
		return nil
	})
	return planned, err
}
