// Package live feeds new notifications to the open streams. Postgres announces
// every stored notification on a channel; the listener hears it, reads the row
// and hands it to the streams of its owner.
package live

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/services/taiko/internal/store"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store/db"
)

const (
	defaultRetryDelay = time.Second
	closeTimeout      = 5 * time.Second
)

// Fanout receives what the listener hears. Reset is called whenever the
// connection was lost or restored, because announcements made in between were
// not heard and the streams must replay what they missed.
type Fanout interface {
	Publish(n db.Notification)
	Reset()
}

// Listener holds one dedicated connection that listens for new notifications,
// and reconnects when it drops.
type Listener struct {
	pool       *pgxpool.Pool
	fan        Fanout
	log        *slog.Logger
	retryDelay time.Duration
}

// Option changes how a Listener behaves.
type Option func(*Listener)

// WithRetryDelay sets how long the listener waits before it reconnects.
func WithRetryDelay(d time.Duration) Option {
	return func(l *Listener) { l.retryDelay = d }
}

// New returns a Listener on pool that feeds fan.
func New(pool *pgxpool.Pool, fan Fanout, log *slog.Logger, opts ...Option) *Listener {
	l := &Listener{pool: pool, fan: fan, log: log, retryDelay: defaultRetryDelay}
	for _, opt := range opts {
		opt(l)
	}
	return l
}

// Start connects and begins listening, then keeps listening in the background.
// It fails if the first connection cannot be made, so a service does not start
// without its live feed. The returned function stops the listener and waits for
// it.
func (l *Listener) Start(ctx context.Context) (func(), error) {
	conn, err := l.connect(ctx)
	if err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		l.run(runCtx, conn)
	}()
	return func() {
		cancel()
		<-done
	}, nil
}

// connect takes a connection out of the pool for good, since a listening
// connection must not be handed to other work, and starts listening on it.
func (l *Listener) connect(ctx context.Context) (*pgx.Conn, error) {
	pooled, err := l.pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("live: acquire connection: %w", err)
	}
	conn := pooled.Hijack()
	if _, err := conn.Exec(ctx, "LISTEN "+store.NotifyChannel); err != nil {
		closeConn(conn)
		return nil, fmt.Errorf("live: listen: %w", err)
	}
	// Anything announced before this point was not heard.
	l.fan.Reset()
	return conn, nil
}

func (l *Listener) run(ctx context.Context, conn *pgx.Conn) {
	for {
		err := l.receive(ctx, conn)
		closeConn(conn)
		if ctx.Err() != nil {
			return
		}
		l.log.Warn("live feed interrupted", slog.Any("error", err))
		l.fan.Reset()
		if conn = l.reconnect(ctx); conn == nil {
			return
		}
	}
}

// reconnect retries until it is listening again, and returns nil when ctx ends
// first.
func (l *Listener) reconnect(ctx context.Context) *pgx.Conn {
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(l.retryDelay):
		}
		conn, err := l.connect(ctx)
		if err == nil {
			return conn
		}
		l.log.Warn("live feed reconnect failed", slog.Any("error", err))
	}
}

// receive hands on notifications until the connection fails or ctx ends.
func (l *Listener) receive(ctx context.Context, conn *pgx.Conn) error {
	repo := store.New(l.pool)
	for {
		n, err := conn.WaitForNotification(ctx)
		if err != nil {
			return err
		}
		owner, id, err := parsePayload(n.Payload)
		if err != nil {
			// A payload nobody understands cannot be fixed by reconnecting.
			l.log.Error("live feed payload dropped", slog.Any("error", err))
			continue
		}
		row, err := repo.Get(ctx, owner, id)
		if err != nil {
			// The row could not be read, so a stream would miss it. Treat it as
			// an interruption: the streams reset and replay it from the table.
			return fmt.Errorf("live: read announced notification: %w", err)
		}
		l.fan.Publish(row)
	}
}

func parsePayload(payload string) (owner, id uuid.UUID, err error) {
	ownerText, idText, ok := strings.Cut(payload, ":")
	if !ok {
		return uuid.Nil, uuid.Nil, errors.New("payload is not <owner>:<id>")
	}
	if owner, err = uuid.Parse(ownerText); err != nil {
		return uuid.Nil, uuid.Nil, errors.New("payload owner is not a uuid")
	}
	if id, err = uuid.Parse(idText); err != nil {
		return uuid.Nil, uuid.Nil, errors.New("payload id is not a uuid")
	}
	return owner, id, nil
}

func closeConn(conn *pgx.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), closeTimeout)
	defer cancel()
	_ = conn.Close(ctx)
}
