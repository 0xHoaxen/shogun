package grpc_test

import (
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
)

// opus-5-5 costs 4 micro-dollars per input token, so a request of n input
// tokens and no output estimates n*4 micro-dollars.
func reserveReq(inputTokens int32) *sorobanv1.ReserveRequest {
	return &sorobanv1.ReserveRequest{
		Service: "fude", Feature: "fude.cover_letter", Model: "claude-opus-5-5",
		EstInputTokens: inputTokens,
	}
}

func TestReserveHoldsEstimateAgainstEveryBudgetInScope(t *testing.T) {
	// Arrange
	h := newHarness(t)

	// Act
	res, err := h.client.Reserve(h.ctx(t), &sorobanv1.ReserveRequest{
		Service: "fude", Feature: "fude.cover_letter", Model: "claude-opus-5-5",
		EstInputTokens: 1000, EstOutputTokens: 500,
	})
	// Assert
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	const wantMicros = 1000*4 + 500*20
	if res.GetEstMicros() != wantMicros {
		t.Errorf("est_micros = %d, want %d", res.GetEstMicros(), wantMicros)
	}
	if _, err := uuid.Parse(res.GetReservationId()); err != nil {
		t.Errorf("reservation_id %q is not a UUID", res.GetReservationId())
	}
	for _, scope := range [][2]string{{"global", ""}, {"service", "fude"}, {"feature", "fude.cover_letter"}} {
		if got := h.reserved(t, scope[0], scope[1]); got != wantMicros {
			t.Errorf("%s %q reserved = %d, want %d", scope[0], scope[1], got, wantMicros)
		}
	}
	if got := h.reserved(t, "service", "tsubame"); got != 0 {
		t.Errorf("an unrelated service budget reserved %d, want 0", got)
	}
}

func TestReserveRefusesWhenAHardBudgetWouldBePassed(t *testing.T) {
	// Arrange
	h := newHarness(t)
	h.seedDefaults(t)
	h.setLimit(t, "global", "", 1000)

	// Act
	_, err := h.client.Reserve(h.ctx(t), reserveReq(1000)) // 4000 micros

	// Assert
	requireStatus(t, err, codes.ResourceExhausted, "BUDGET_EXHAUSTED")
	info := errorInfo(err)
	if info.GetMetadata()["budget_id"] == "" {
		t.Error("ErrorInfo has no budget_id")
	}
	if got, want := info.GetMetadata()["resets_at"], "2026-10-31T18:30:00Z"; got != want {
		t.Errorf("resets_at = %q, want %q", got, want)
	}
	for _, scope := range [][2]string{{"global", ""}, {"service", "fude"}, {"feature", "fude.cover_letter"}} {
		if got := h.reserved(t, scope[0], scope[1]); got != 0 {
			t.Errorf("%s %q reserved %d after a refusal, want 0", scope[0], scope[1], got)
		}
	}
}

func TestReserveEmitsBudgetExhaustedOncePerPeriod(t *testing.T) {
	// Arrange
	h := newHarness(t)
	h.seedDefaults(t)
	h.setLimit(t, "global", "", 1000)

	// Act
	for range 3 {
		_, err := h.client.Reserve(h.ctx(t), reserveReq(1000))
		requireStatus(t, err, codes.ResourceExhausted, "BUDGET_EXHAUSTED")
	}

	// Assert
	if n := h.outboxCount(t, "cost.budget_exhausted"); n != 1 {
		t.Fatalf("got %d cost.budget_exhausted rows, want 1", n)
	}
}

func TestReserveNeverBlocksOnASoftBudget(t *testing.T) {
	// Arrange: the feature budget is soft, so a zero limit only notifies.
	h := newHarness(t)
	h.seedDefaults(t)
	h.setLimit(t, "feature", "fude.cover_letter", 0)

	// Act
	_, err := h.client.Reserve(h.ctx(t), reserveReq(1000))
	// Assert
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if h.outboxCount(t, "") != 0 {
		t.Error("a soft budget wrote an event")
	}
}

func TestReserveRejectsBadRequests(t *testing.T) {
	tests := []struct {
		name   string
		req    *sorobanv1.ReserveRequest
		reason string
	}{
		{"missing service", &sorobanv1.ReserveRequest{Feature: "fude.post", Model: "claude-opus-5-5"}, "INVALID_REQUEST"},
		{"missing model", &sorobanv1.ReserveRequest{Service: "fude", Feature: "fude.post"}, "INVALID_REQUEST"},
		{"feature of another service", &sorobanv1.ReserveRequest{Service: "fude", Feature: "katana.suggest", Model: "claude-opus-5-5"}, "FEATURE_SERVICE_MISMATCH"},
		{"negative estimate", &sorobanv1.ReserveRequest{Service: "fude", Feature: "fude.post", Model: "claude-opus-5-5", EstInputTokens: -1}, "INVALID_TOKEN_COUNT"},
		{"model without a price", &sorobanv1.ReserveRequest{Service: "fude", Feature: "fude.post", Model: "claude-unpriced"}, "MODEL_PRICE_UNKNOWN"},
	}
	h := newHarness(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.Reserve(h.ctx(t), tt.req)
			requireStatus(t, err, codes.InvalidArgument, tt.reason)
		})
	}
}

func TestReserveRequiresIdentity(t *testing.T) {
	h := newHarness(t)

	_, err := h.client.Reserve(context.Background(), reserveReq(1))

	requireStatus(t, err, codes.Unauthenticated, "")
}

func TestReserveCopiesDefaultBudgetsOnceUnderConcurrentFirstCalls(t *testing.T) {
	// Arrange
	h := newHarness(t)
	const calls = 10

	// Act
	var wg sync.WaitGroup
	errs := make(chan error, calls)
	for range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.client.Reserve(h.ctx(t), reserveReq(10))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)

	// Assert
	for err := range errs {
		if err != nil {
			t.Fatalf("Reserve: %v", err)
		}
	}
	var budgets int
	if err := h.pool.QueryRow(context.Background(),
		`SELECT count(*) FROM budgets WHERE owner_id = $1`, h.owner).Scan(&budgets); err != nil {
		t.Fatalf("count budgets: %v", err)
	}
	if budgets != 11 {
		t.Errorf("owner has %d budgets, want the 11 defaults", budgets)
	}
}

func TestReserveNeverPassesAHardLimitUnderConcurrency(t *testing.T) {
	// Arrange: a $1 global limit and 50 requests of 100000 micro-dollars each,
	// so exactly 10 fit.
	h := newHarness(t)
	h.seedDefaults(t)
	const (
		limit    = 1_000_000
		requests = 50
		inputs   = 25_000 // 25000 tokens * 4 micros = 100000 micros
		perCall  = 100_000
	)
	h.setLimit(t, "global", "", limit)

	// Act
	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		granted  int
		refusals int
		other    []error
	)
	for range requests {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.client.Reserve(h.ctx(t), reserveReq(inputs))
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				granted++
			case errorInfo(err).GetReason() == "BUDGET_EXHAUSTED":
				refusals++
			default:
				other = append(other, err)
			}
		}()
	}
	wg.Wait()

	// Assert
	if len(other) > 0 {
		t.Fatalf("unexpected errors: %v", other)
	}
	if granted != limit/perCall || refusals != requests-granted {
		t.Errorf("granted %d and refused %d, want %d and %d", granted, refusals, limit/perCall, requests-granted)
	}
	if got := h.reserved(t, "global", ""); got != int64(granted)*perCall || got > limit {
		t.Errorf("global reserved = %d, want %d and at most %d", got, int64(granted)*perCall, limit)
	}
	if n := h.outboxCount(t, "cost.budget_exhausted"); n != 1 {
		t.Errorf("got %d cost.budget_exhausted rows, want 1", n)
	}
}
