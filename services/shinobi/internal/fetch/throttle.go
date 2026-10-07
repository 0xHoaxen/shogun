package fetch

import (
	"context"
	"sync"
	"time"
)

// DefaultInterval is the least time between two requests to one host.
const DefaultInterval = time.Second

// throttle spaces requests to the same host, so a source is never hit faster
// than its operator would expect from a polite reader.
type throttle struct {
	mu       sync.Mutex
	interval time.Duration
	last     map[string]time.Time
	now      func() time.Time
	sleep    func(ctx context.Context, d time.Duration) error
}

func newThrottle(interval time.Duration) *throttle {
	return &throttle{interval: interval, last: map[string]time.Time{}, now: time.Now, sleep: sleepContext}
}

// wait blocks until a request to host may be made, and records it.
func (t *throttle) wait(ctx context.Context, host string) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if last, ok := t.last[host]; ok {
		if d := t.interval - t.now().Sub(last); d > 0 {
			if err := t.sleep(ctx, d); err != nil {
				return err
			}
		}
	}
	t.last[host] = t.now()
	return nil
}

func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
