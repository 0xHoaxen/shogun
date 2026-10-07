package grpc_test

import (
	"testing"

	"google.golang.org/grpc/codes"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
)

// requireWithinLimit fails if the global budget holds more than limit, spent
// and reserved together.
func requireWithinLimit(t *testing.T, h *harness, limit int64, step string) {
	t.Helper()
	if held := h.spent(t, "global", "") + h.reserved(t, "global", ""); held > limit {
		t.Fatalf("%s: global spent+reserved = %d, over the limit %d", step, held, limit)
	}
}

func TestDrillExhaustedBudgetRefusesOnceAndRecoversWithoutPassingTheLimit(t *testing.T) {
	// Arrange: a global limit of 4000 micro-dollars (1000 input tokens).
	h := newHarness(t)
	h.seedDefaults(t)
	const limit = 4000
	h.setLimit(t, "global", "", limit)

	// Act and assert, step by step.
	first := h.reserve(t, 500) // 2000 held
	second := h.reserve(t, 500)
	requireWithinLimit(t, h, limit, "two holds")

	for range 3 {
		_, err := h.client.Reserve(h.ctx(t), reserveReq(1))
		requireStatus(t, err, codes.ResourceExhausted, "BUDGET_EXHAUSTED")
	}
	requireWithinLimit(t, h, limit, "after refusals")
	if n := h.outboxCount(t, "cost.budget_exhausted"); n != 1 {
		t.Fatalf("got %d cost.budget_exhausted rows after three refusals, want 1", n)
	}

	// The call that ran used less than it held: 300 tokens (1200) of 2000.
	h.commit(t, first, 300)
	if got := h.spent(t, "global", ""); got != 1200 {
		t.Errorf("spent = %d after the commit, want 1200", got)
	}
	h.reserve(t, 200) // 800 fits exactly into the 800 left: 1200 + 2000 + 800
	requireWithinLimit(t, h, limit, "after the commit freed room")
	_, err := h.client.Reserve(h.ctx(t), reserveReq(1))
	requireStatus(t, err, codes.ResourceExhausted, "BUDGET_EXHAUSTED")

	// The call that failed gives its whole hold back, so a new call fits.
	if _, err := h.client.Release(h.ctx(t), &sorobanv1.ReleaseRequest{ReservationId: second}); err != nil {
		t.Fatalf("Release: %v", err)
	}
	h.reserve(t, 500)
	requireWithinLimit(t, h, limit, "after the release")

	// Assert: one refusal event for the whole period, however many refusals.
	if n := h.outboxCount(t, "cost.budget_exhausted"); n != 1 {
		t.Errorf("got %d cost.budget_exhausted rows, want 1 for the period", n)
	}
}
