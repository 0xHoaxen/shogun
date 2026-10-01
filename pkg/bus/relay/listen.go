package relay

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/0xHoaxen/shogun/pkg/outbox"
)

const (
	listenRetryDelay = time.Second
	// notifyDebounce coalesces the one-NOTIFY-per-event burst of a large
	// transaction into a single relay run.
	notifyDebounce = 50 * time.Millisecond
)

// listen waits for NOTIFY on the outbox channel and kicks the relay for each
// wake-up. The channel is database-wide, so notifications from other services
// sharing the database only cause a harmless extra run. If the connection
// drops it reconnects; the periodic job covers any missed notifications.
func (r *Relay) listen(ctx context.Context) {
	for ctx.Err() == nil {
		if err := r.listenOnce(ctx); err != nil && ctx.Err() == nil {
			r.log.Warn("relay: listen connection lost", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(listenRetryDelay):
		}
	}
}

func (r *Relay) listenOnce(ctx context.Context) error {
	// A dedicated connection: pooled connections must not be held by LISTEN.
	conn, err := pgx.ConnectConfig(ctx, r.cfg.Pool.Config().ConnConfig.Copy())
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()

	if _, err := conn.Exec(ctx, "LISTEN "+pgx.Identifier{outbox.Channel}.Sanitize()); err != nil {
		return err
	}
	for {
		if _, err := conn.WaitForNotification(ctx); err != nil {
			return err
		}
		if err := drainNotifications(ctx, conn); err != nil {
			return err
		}
		r.kick(ctx)
	}
}

// drainNotifications swallows notifications arriving within notifyDebounce.
func drainNotifications(ctx context.Context, conn *pgx.Conn) error {
	for {
		waitCtx, cancel := context.WithTimeout(ctx, notifyDebounce)
		_, err := conn.WaitForNotification(waitCtx)
		cancel()
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if errors.Is(err, context.DeadlineExceeded) && !conn.PgConn().IsClosed() {
			return nil
		}
		return err
	}
}
