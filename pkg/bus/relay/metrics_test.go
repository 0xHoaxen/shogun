package relay_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/riverqueue/river/rivertype"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	"github.com/0xHoaxen/shogun/pkg/bus/relay"
)

// deadBus never reaches its consumer.
type deadBus struct{}

func (deadBus) Deliver(context.Context, string, *eventsv1.Envelope) error {
	return errors.New("consumer is down")
}

// slowRetry keeps a failed delivery waiting for the whole test.
type slowRetry struct{}

func (slowRetry) NextRetry(*rivertype.JobRow) time.Time { return time.Now().Add(time.Hour) }

func gather(t *testing.T, pool *pgxpool.Pool) map[string]float64 {
	t.Helper()
	reg := prometheus.NewPedanticRegistry()
	reg.MustRegister(relay.NewMetrics(pool, nil))
	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	got := map[string]float64{}
	for _, f := range families {
		for _, m := range f.GetMetric() {
			key := f.GetName()
			for _, l := range m.GetLabel() {
				key += "{" + l.GetName() + "=" + l.GetValue() + "}"
			}
			got[key] = m.GetGauge().GetValue()
		}
	}
	return got
}

func ageAll(ctx context.Context, t *testing.T, pool *pgxpool.Pool, table string, by time.Duration) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE `+table+` SET created_at = created_at - $1::interval`, by.String()); err != nil {
		t.Fatalf("age %s: %v", table, err)
	}
}

func TestMetricsLagIsTheAgeOfTheOldestEventNotYetRelayed(t *testing.T) {
	// Arrange: events written ten minutes ago, with no relay running.
	ctx, pool := newPool(t)
	writeEvents(ctx, t, pool, "job.added", 2)
	ageAll(ctx, t, pool, "outbox", 10*time.Minute)

	// Act
	got := gather(t, pool)

	// Assert
	if lag := got["shogun_outbox_lag_seconds"]; lag < 600 || lag > 660 {
		t.Errorf("lag = %v, want about 600", lag)
	}
}

func TestMetricsLagKeepsGrowingWhileAConsumerHasNotAcknowledged(t *testing.T) {
	// Arrange: every outbox row is relayed, but the only consumer is down.
	ctx, pool := newPool(t)
	writeEvents(ctx, t, pool, "job.added", 1)
	startRelay(ctx, t, relay.Config{
		Pool: pool, Bus: deadBus{}, Interval: time.Hour, RetryPolicy: slowRetry{},
		FetchPollInterval: 100 * time.Millisecond,
		Routes:            routes(map[string][]string{"job.added": {"taiko"}}),
		Registerer:        prometheus.NewRegistry(),
	})
	waitUndelivered(ctx, t, pool, 0)
	ageAll(ctx, t, pool, "river_job", 7*time.Minute)

	// Act
	got := gather(t, pool)

	// Assert: the outbox is empty of undelivered rows, yet the lag is the wait.
	if lag := got["shogun_outbox_lag_seconds"]; lag < 420 {
		t.Errorf("lag = %v with an unacknowledged delivery, want at least 420", lag)
	}
}

func TestMetricsLagIsZeroOnceEveryConsumerHasTheEvent(t *testing.T) {
	// Arrange
	ctx, pool := newPool(t)
	fake := newFakeBus(nil)
	writeEvents(ctx, t, pool, "job.added", 3)
	startRelay(ctx, t, relay.Config{
		Pool: pool, Bus: fake, Interval: time.Hour, RetryPolicy: fastRetry{},
		FetchPollInterval: 100 * time.Millisecond,
		Routes:            routes(map[string][]string{"job.added": {"taiko"}}),
		Registerer:        prometheus.NewRegistry(),
	})
	fake.waitDelivered(t, 3)

	// Act and assert: River finishes the jobs a moment after the handler returns.
	deadline := time.Now().Add(waitFor)
	for {
		lag := gather(t, pool)["shogun_outbox_lag_seconds"]
		if lag == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("lag = %v after every delivery, want 0", lag)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestMetricsCountDiscardedJobsByKind(t *testing.T) {
	// Arrange: one attempt each, and the consumer is down.
	ctx, pool := newPool(t)
	writeEvents(ctx, t, pool, "job.added", 2)
	startRelay(ctx, t, relay.Config{
		Pool: pool, Bus: deadBus{}, Interval: time.Hour, RetryPolicy: fastRetry{}, MaxAttempts: 1,
		FetchPollInterval: 100 * time.Millisecond,
		Routes:            routes(map[string][]string{"job.added": {"taiko"}}),
		Registerer:        prometheus.NewRegistry(),
	})

	// Act and assert
	deadline := time.Now().Add(waitFor)
	for {
		discarded := gather(t, pool)["shogun_river_jobs_discarded{kind=deliver_event}"]
		if discarded == 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("discarded deliver_event jobs = %v, want 2", discarded)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestMetricsCountNoDiscardedJobsWhenNothingFailed(t *testing.T) {
	// Arrange
	_, pool := newPool(t)

	// Act
	got := gather(t, pool)

	// Assert: no series, so an alert on > 0 stays quiet.
	for key := range got {
		if key != "shogun_outbox_lag_seconds" {
			t.Errorf("unexpected series %s on a quiet relay", key)
		}
	}
}
