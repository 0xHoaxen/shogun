package relay_test

import (
	"context"
	"testing"
	"time"

	"github.com/riverqueue/river"

	"github.com/0xHoaxen/shogun/pkg/bus/relay"
)

const serviceQueue = "service"

type probeArgs struct{}

func (probeArgs) Kind() string { return "probe" }

type probeWorker struct {
	river.WorkerDefaults[probeArgs]
	ran chan struct{}
}

func (w *probeWorker) Work(context.Context, *river.Job[probeArgs]) error {
	select {
	case w.ran <- struct{}{}:
	default:
	}
	return nil
}

func TestRelayRunsAServicesOwnPeriodicJobOnTheSharedClient(t *testing.T) {
	ctx, pool := newPool(t)
	worker := &probeWorker{ran: make(chan struct{}, 1)}

	startRelay(ctx, t, relay.Config{
		Pool:    pool,
		Bus:     newFakeBus(nil),
		Workers: func(ws *river.Workers) { river.AddWorker(ws, worker) },
		Queues:  map[string]river.QueueConfig{serviceQueue: {MaxWorkers: 1}},
		PeriodicJobs: []*river.PeriodicJob{
			river.NewPeriodicJob(
				river.PeriodicInterval(time.Hour),
				func() (river.JobArgs, *river.InsertOpts) { return probeArgs{}, &river.InsertOpts{Queue: serviceQueue} },
				&river.PeriodicJobOpts{RunOnStart: true},
			),
		},
	})

	select {
	case <-worker.ran:
	case <-time.After(waitFor):
		t.Fatal("the service's periodic job did not run")
	}
}

func TestNewRejectsAServiceQueueThatShadowsTheRelays(t *testing.T) {
	_, pool := newPool(t)

	for _, name := range []string{"relay", "deliver"} {
		_, err := relay.New(relay.Config{
			Pool: pool, Bus: newFakeBus(nil),
			Queues: map[string]river.QueueConfig{name: {MaxWorkers: 1}},
		})
		if err == nil {
			t.Fatalf("queue %q was accepted", name)
		}
	}
}
