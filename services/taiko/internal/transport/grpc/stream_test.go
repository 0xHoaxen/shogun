package grpc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	taikov1 "github.com/0xHoaxen/shogun/gen/go/shogun/taiko/v1"
)

const (
	liveDeadline     = time.Second
	recoveryDeadline = 10 * time.Second
)

// openStream subscribes as owner and waits for the server to say the stream is
// registered, so a notification created afterwards is not missed.
func (h *harness) openStream(ctx context.Context, t *testing.T, owner, after string) taikov1.TaikoService_SubscribeClient {
	t.Helper()
	stream, err := h.client.Subscribe(h.ctxFrom(ctx, t, owner), &taikov1.SubscribeRequest{AfterId: after})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	if _, err := stream.Header(); err != nil {
		t.Fatalf("waiting for the stream to register: %v", err)
	}
	return stream
}

func recvWithin(t *testing.T, stream taikov1.TaikoService_SubscribeClient, cancel context.CancelFunc, d time.Duration) *taikov1.Notification {
	t.Helper()
	timer := time.AfterFunc(d, cancel)
	defer timer.Stop()
	res, err := stream.Recv()
	if err != nil {
		t.Fatalf("no notification within %s: %v", d, err)
	}
	return res.GetNotification()
}

func TestSubscribeReceivesAConsumedEventWithinOneSecond(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stream := h.openStream(ctx, t, h.owner, "")

	start := time.Now()
	h.draftReady(t, h.owner)
	got := recvWithin(t, stream, cancel, liveDeadline)

	if got.GetTitle() != "Cover letter ready" || got.GetId() == "" {
		t.Fatalf("got %+v", got)
	}
	if elapsed := time.Since(start); elapsed > liveDeadline {
		t.Fatalf("took %s, want under %s", elapsed, liveDeadline)
	}
}

func TestSubscribeReplaysWhatWasMissedThenGoesLiveWithoutRepeats(t *testing.T) {
	h := newHarness(t)
	for range 3 {
		h.draftReady(t, h.owner)
	}
	stored := h.list(t, &taikov1.ListRequest{}).GetNotifications() // newest first
	seen := stored[2].GetId()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	stream := h.openStream(ctx, t, h.owner, seen)
	first := recvWithin(t, stream, cancel, liveDeadline)
	second := recvWithin(t, stream, cancel, liveDeadline)
	h.draftReady(t, h.owner)
	third := recvWithin(t, stream, cancel, liveDeadline)

	if first.GetId() != stored[1].GetId() || second.GetId() != stored[0].GetId() {
		t.Fatalf("replayed %s, %s; want %s, %s (oldest first, after the seen one)", first.GetId(), second.GetId(), stored[1].GetId(), stored[0].GetId())
	}
	if third.GetId() == first.GetId() || third.GetId() == second.GetId() || third.GetId() == seen {
		t.Fatalf("live notification %s repeats an earlier one", third.GetId())
	}
}

func TestSubscribeOnlyStreamsTheOwnersNotifications(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stream := h.openStream(ctx, t, h.owner, "")

	h.draftReady(t, uuid.NewString())
	h.draftReady(t, h.owner)
	got := recvWithin(t, stream, cancel, liveDeadline)

	mine := h.list(t, &taikov1.ListRequest{}).GetNotifications()
	if len(mine) != 1 || got.GetId() != mine[0].GetId() {
		t.Fatalf("streamed %s, but my only notification is %v", got.GetId(), mine)
	}
}

func TestSubscribeRejectsABadAfterID(t *testing.T) {
	h := newHarness(t)
	stream, err := h.client.Subscribe(h.ctx(t), &taikov1.SubscribeRequest{AfterId: "nope"})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	_, err = stream.Recv()

	requireStatus(t, err, codes.InvalidArgument, "INVALID_ID")
}

// receiveAll collects want notifications for owner after the id after,
// reconnecting from the last id it saw whenever the server resets the stream,
// as a client must.
func (h *harness) receiveAll(t *testing.T, owner, after string, want int) []*taikov1.Notification {
	t.Helper()
	deadline := time.Now().Add(recoveryDeadline)
	var got []*taikov1.Notification
	last := after
	for len(got) < want {
		if time.Now().After(deadline) {
			t.Fatalf("got %d of %d notifications before the deadline", len(got), want)
		}
		ctx, cancel := context.WithDeadline(t.Context(), deadline)
		stream := h.openStream(ctx, t, owner, last)
		for len(got) < want {
			res, err := stream.Recv()
			if err != nil {
				if st, _ := status.FromError(err); st.Code() == codes.Unavailable {
					break // reset: reconnect from the last id seen
				}
				cancel()
				t.Fatalf("recv: %v", err)
			}
			got = append(got, res.GetNotification())
			last = res.GetNotification().GetId()
		}
		cancel()
	}
	return got
}

func TestStreamsResetAndLoseNothingWhenTheLiveFeedConnectionDrops(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	stream := h.openStream(ctx, t, h.owner, "")
	h.draftReady(t, h.owner)
	before := recvWithin(t, stream, cancel, liveDeadline)

	killed := h.killLiveFeedConnection(t)
	_, err := stream.Recv()
	h.draftReady(t, h.owner) // while the feed may still be down
	afterDrop := h.receiveAll(t, h.owner, before.GetId(), 1)
	h.draftReady(t, h.owner)
	afterRecovery := h.receiveAll(t, h.owner, afterDrop[0].GetId(), 1)

	if killed == 0 {
		t.Fatal("found no live feed connection to drop")
	}
	if st, _ := status.FromError(err); st.Code() != codes.Unavailable {
		t.Fatalf("stream ended with %v, want Unavailable so the client reconnects", err)
	}
	if afterDrop[0].GetId() == before.GetId() || afterRecovery[0].GetId() == afterDrop[0].GetId() {
		t.Fatalf("repeated notifications: %s, %s, %s", before.GetId(), afterDrop[0].GetId(), afterRecovery[0].GetId())
	}
	if all := h.list(t, &taikov1.ListRequest{}); len(all.GetNotifications()) != 3 {
		t.Fatalf("stored %d notifications, want 3", len(all.GetNotifications()))
	}
}

// killLiveFeedConnection terminates the connection that listens for new
// notifications and returns how many it terminated.
func (h *harness) killLiveFeedConnection(t *testing.T) int {
	t.Helper()
	rows, err := h.pool.Query(context.Background(),
		`SELECT pg_terminate_backend(pid) FROM pg_stat_activity
		 WHERE datname = current_database() AND pid <> pg_backend_pid() AND query = 'LISTEN taiko_notifications'`)
	if err != nil {
		t.Fatalf("terminate: %v", err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		n++
	}
	if err := rows.Err(); err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("terminate: %v", err)
	}
	return n
}
