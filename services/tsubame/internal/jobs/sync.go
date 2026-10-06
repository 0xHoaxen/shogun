package jobs

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"
)

const (
	// Queue is the River queue of tsubame's own jobs.
	Queue = "tsubame"

	kindGmailSync = "gmail_sync"

	// syncEvery is how often every account is synced.
	syncEvery = 5 * time.Minute
	// syncTimeout bounds one run over all accounts.
	syncTimeout = 4 * time.Minute
)

// Syncer is what the sync job needs from the use case.
type Syncer interface {
	SyncAll(ctx context.Context) error
}

// activeStates are the states in which a sync counts as already queued. River
// requires all of them; leaving out completed ones lets the next run in.
var activeStates = []rivertype.JobState{
	rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning,
	rivertype.JobStateRetryable, rivertype.JobStateScheduled,
}

// GmailSyncArgs are the args of gmail_sync; it has none. The periodic job
// inserts one every five minutes, and at most one waits at a time.
type GmailSyncArgs struct{}

// Kind implements river.JobArgs.
func (GmailSyncArgs) Kind() string { return kindGmailSync }

// InsertOpts implements river.JobArgsWithInsertOpts.
func (GmailSyncArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue:       Queue,
		MaxAttempts: 1, // the next run is five minutes away; a retry would only overlap it
		UniqueOpts:  river.UniqueOpts{ByArgs: true, ByState: activeStates},
	}
}

type syncWorker struct {
	river.WorkerDefaults[GmailSyncArgs]
	syncer Syncer
	log    *slog.Logger
}

func (w *syncWorker) Timeout(*river.Job[GmailSyncArgs]) time.Duration { return syncTimeout }

// Work syncs every active account. Failures are logged per account by the
// syncer; the job reports them so they show up in River, and the next run
// tries again from the same cursors.
func (w *syncWorker) Work(ctx context.Context, _ *river.Job[GmailSyncArgs]) error {
	if err := w.syncer.SyncAll(ctx); err != nil {
		return fmt.Errorf("gmail sync: %w", err)
	}
	return nil
}

// Setup is what the outbox relay needs to run tsubame's jobs on its River
// client: pass the fields to relay.Config.
type Setup struct {
	Workers      func(*river.Workers)
	Queues       map[string]river.QueueConfig
	PeriodicJobs []*river.PeriodicJob
}

// NewSetup builds the scheduled sync and the classification worker.
func NewSetup(syncer Syncer, classifier Classifier, log *slog.Logger) Setup {
	return Setup{
		Workers: func(ws *river.Workers) {
			river.AddWorker(ws, &syncWorker{syncer: syncer, log: log})
			river.AddWorker(ws, &classifyWorker{classifier: classifier, log: log, now: time.Now})
		},
		Queues: map[string]river.QueueConfig{Queue: {MaxWorkers: classifyWorkers}},
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(river.PeriodicInterval(syncEvery), func() (river.JobArgs, *river.InsertOpts) {
				return GmailSyncArgs{}, nil
			}, &river.PeriodicJobOpts{RunOnStart: true}),
		},
	}
}
