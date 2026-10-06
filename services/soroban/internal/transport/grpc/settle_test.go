package grpc_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/services/soroban/internal/app"
)

var scopes = [][2]string{{"global", ""}, {"service", "fude"}, {"feature", "fude.cover_letter"}}

// reserve holds inputTokens' worth of opus-5-5 input and returns the id.
func (h *harness) reserve(t *testing.T, inputTokens int32) string {
	t.Helper()
	res, err := h.client.Reserve(h.ctx(t), reserveReq(inputTokens))
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	return res.GetReservationId()
}

func (h *harness) commit(t *testing.T, id string, inputTokens int32) *sorobanv1.LedgerEntry {
	t.Helper()
	res, err := h.client.Commit(h.ctx(t), &sorobanv1.CommitRequest{
		ReservationId: id, RequestId: "req-1", Usage: &sorobanv1.Usage{InputTokens: inputTokens},
	})
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return res.GetEntry()
}

// spent returns the spent total of the owner's budget with the scope.
func (h *harness) spent(t *testing.T, scopeType, scopeValue string) int64 {
	t.Helper()
	var n int64
	err := h.pool.QueryRow(context.Background(),
		`SELECT COALESCE(sum(p.spent_micros), 0)
		   FROM budget_periods p JOIN budgets b ON b.id = p.budget_id
		  WHERE b.owner_id = $1 AND b.scope_type = $2 AND b.scope_value = $3`,
		h.owner, scopeType, scopeValue).Scan(&n)
	if err != nil {
		t.Fatalf("spent: %v", err)
	}
	return n
}

func (h *harness) reservationStatus(t *testing.T, id string) string {
	t.Helper()
	var status string
	if err := h.pool.QueryRow(context.Background(),
		`SELECT status FROM reservations WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatalf("status: %v", err)
	}
	return status
}

func TestCommitMovesReservedToSpentAndWritesTheLedger(t *testing.T) {
	// Arrange: hold 1000 input tokens (4000 micros), then use only 600.
	h := newHarness(t)
	id := h.reserve(t, 1000)

	// Act
	entry := h.commit(t, id, 600)

	// Assert
	if entry.GetCostMicros() != 2400 || entry.GetUsage().GetInputTokens() != 600 {
		t.Errorf("entry = %v, want 600 tokens costing 2400 micros", entry)
	}
	for _, scope := range scopes {
		if got := h.reserved(t, scope[0], scope[1]); got != 0 {
			t.Errorf("%s reserved = %d after commit, want 0", scope[0], got)
		}
		if got := h.spent(t, scope[0], scope[1]); got != 2400 {
			t.Errorf("%s spent = %d, want 2400", scope[0], got)
		}
	}
	if got := h.reservationStatus(t, id); got != "committed" {
		t.Errorf("reservation status = %q, want committed", got)
	}
}

func TestCommitTwiceReturnsTheSameEntryAndCountsOnce(t *testing.T) {
	h := newHarness(t)
	id := h.reserve(t, 1000)

	first := h.commit(t, id, 600)
	second := h.commit(t, id, 600)

	if first.GetId() != second.GetId() {
		t.Errorf("second commit wrote entry %s, want %s", second.GetId(), first.GetId())
	}
	if got := h.spent(t, "global", ""); got != 2400 {
		t.Errorf("global spent = %d, want 2400", got)
	}
}

func TestReleaseGivesTheHoldBackAndIsIdempotent(t *testing.T) {
	// Arrange
	h := newHarness(t)
	id := h.reserve(t, 1000)

	// Act
	for range 2 {
		if _, err := h.client.Release(h.ctx(t), &sorobanv1.ReleaseRequest{ReservationId: id}); err != nil {
			t.Fatalf("Release: %v", err)
		}
	}

	// Assert
	for _, scope := range scopes {
		if got := h.reserved(t, scope[0], scope[1]); got != 0 {
			t.Errorf("%s reserved = %d after release, want 0", scope[0], got)
		}
	}
	if got := h.reservationStatus(t, id); got != "released" {
		t.Errorf("reservation status = %q, want released", got)
	}
}

func TestCommitAfterReleaseIsRefused(t *testing.T) {
	h := newHarness(t)
	id := h.reserve(t, 1000)
	if _, err := h.client.Release(h.ctx(t), &sorobanv1.ReleaseRequest{ReservationId: id}); err != nil {
		t.Fatalf("Release: %v", err)
	}

	_, err := h.client.Commit(h.ctx(t), &sorobanv1.CommitRequest{ReservationId: id, Usage: &sorobanv1.Usage{InputTokens: 1}})

	requireStatus(t, err, codes.FailedPrecondition, "RESERVATION_NOT_OPEN")
}

func TestSettlingRejectsUnknownAndForeignReservations(t *testing.T) {
	h := newHarness(t)
	id := h.reserve(t, 10)
	stranger := h.ctxFor(t, uuid.NewString())
	commit := &sorobanv1.CommitRequest{ReservationId: id, Usage: &sorobanv1.Usage{InputTokens: 1}}

	tests := []struct {
		name   string
		call   func() error
		code   codes.Code
		reason string
	}{
		{"commit of a random id", func() error {
			_, err := h.client.Commit(h.ctx(t), &sorobanv1.CommitRequest{ReservationId: uuid.NewString()})
			return err
		}, codes.NotFound, "RESOURCE_NOT_FOUND"},
		{"commit of another owner's reservation", func() error {
			_, err := h.client.Commit(stranger, commit)
			return err
		}, codes.NotFound, "RESOURCE_NOT_FOUND"},
		{"release of another owner's reservation", func() error {
			_, err := h.client.Release(stranger, &sorobanv1.ReleaseRequest{ReservationId: id})
			return err
		}, codes.NotFound, "RESOURCE_NOT_FOUND"},
		{"commit with a malformed id", func() error {
			_, err := h.client.Commit(h.ctx(t), &sorobanv1.CommitRequest{ReservationId: "nope"})
			return err
		}, codes.InvalidArgument, "INVALID_REQUEST"},
		{"commit with negative tokens", func() error {
			_, err := h.client.Commit(h.ctx(t), &sorobanv1.CommitRequest{ReservationId: id, Usage: &sorobanv1.Usage{OutputTokens: -1}})
			return err
		}, codes.InvalidArgument, "INVALID_TOKEN_COUNT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			requireStatus(t, tt.call(), tt.code, tt.reason)
		})
	}
}

func TestThresholdEventsAreWrittenOncePerPeriodAndThreshold(t *testing.T) {
	// Arrange: a $1 global budget with 50/80/100 thresholds; the service and
	// feature budgets are raised so that only the global one speaks.
	h := newHarness(t)
	h.seedDefaults(t)
	h.setLimit(t, "global", "", 1_000_000)
	h.setLimit(t, "service", "fude", 100_000_000)
	h.setLimit(t, "feature", "fude.cover_letter", 100_000_000)
	const (
		tokens500k = 125_000 // 4 micros per token
		tokens400k = 100_000
		tokens100k = 25_000
	)

	// Act and assert, step by step: spend 50%, then 90%, then 100%.
	steps := []struct {
		name       string
		tokens     int32
		wantEvents int
	}{
		{"50% crosses the first threshold", tokens500k, 1},
		{"90% crosses the 80% threshold", tokens400k, 2},
		{"100% crosses the last threshold", tokens100k, 3},
	}
	for _, step := range steps {
		h.commit(t, h.reserve(t, step.tokens), step.tokens)
		if got := h.outboxCount(t, "cost.threshold_reached"); got != step.wantEvents {
			t.Fatalf("%s: %d threshold events, want %d", step.name, got, step.wantEvents)
		}
	}
}

func TestSweeperExpiresOnlyOpenReservationsPastTheirExpiry(t *testing.T) {
	// Arrange: one open reservation and one already committed.
	h := newHarness(t)
	open := h.reserve(t, 1000)
	h.commit(t, h.reserve(t, 500), 500)
	later := app.NewService(h.pool, func() time.Time { return fixedNow.Add(11 * time.Minute) })

	// Act
	expired, err := later.ExpireReservations(context.Background())
	// Assert
	if err != nil {
		t.Fatalf("ExpireReservations: %v", err)
	}
	if expired != 1 {
		t.Errorf("expired %d reservations, want 1", expired)
	}
	if got := h.reservationStatus(t, open); got != "expired" {
		t.Errorf("reservation status = %q, want expired", got)
	}
	for _, scope := range scopes {
		if got := h.reserved(t, scope[0], scope[1]); got != 0 {
			t.Errorf("%s reserved = %d after the sweep, want 0", scope[0], got)
		}
	}
}

func TestSweeperLeavesReservationsBeforeTheirExpiry(t *testing.T) {
	h := newHarness(t)
	h.reserve(t, 1000)
	early := app.NewService(h.pool, func() time.Time { return fixedNow.Add(9 * time.Minute) })

	expired, err := early.ExpireReservations(context.Background())

	if err != nil || expired != 0 {
		t.Fatalf("expired %d, %v; want 0", expired, err)
	}
	if got := h.reserved(t, "global", ""); got != 4000 {
		t.Errorf("global reserved = %d, want 4000", got)
	}
}

func TestCommitAfterExpiryStillRecordsTheSpend(t *testing.T) {
	// Arrange: the hold expired, but the call it was for ran.
	h := newHarness(t)
	id := h.reserve(t, 1000)
	later := app.NewService(h.pool, func() time.Time { return fixedNow.Add(11 * time.Minute) })
	if _, err := later.ExpireReservations(context.Background()); err != nil {
		t.Fatalf("ExpireReservations: %v", err)
	}

	// Act
	entry := h.commit(t, id, 1000)

	// Assert
	if entry.GetCostMicros() != 4000 {
		t.Errorf("cost = %d, want 4000", entry.GetCostMicros())
	}
	if got := h.spent(t, "global", ""); got != 4000 {
		t.Errorf("global spent = %d, want 4000", got)
	}
	if got := h.reserved(t, "global", ""); got != 0 {
		t.Errorf("global reserved = %d, want 0 (the hold was already given back)", got)
	}
}
