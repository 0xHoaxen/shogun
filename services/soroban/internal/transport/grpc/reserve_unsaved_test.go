package grpc_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
)

// testReq is a connection test of details the caller has not saved.
func testReq(model string) *sorobanv1.ReserveRequest {
	m := model
	return &sorobanv1.ReserveRequest{
		Service: "fude", Feature: "fude.test_connection", EstInputTokens: 20, EstOutputTokens: 16, UnsavedModel: &m,
	}
}

func (h *harness) count(t *testing.T, query string, args ...any) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(), query, args...).Scan(&n); err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestAnUnsavedModelCanBeTestedByAUserWithNoProvider(t *testing.T) {
	// Arrange
	h := newBareHarness(t)

	// Act
	res, err := h.client.Reserve(h.ctx(t), testReq("my-llama-70b"))
	// Assert
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if res.GetProvider() != nil {
		t.Fatalf("provider = %v, want none: the caller brings its own", res.GetProvider())
	}
	if got := h.reservationModel(t, res.GetReservationId()); got != "my-llama-70b" {
		t.Fatalf("the reservation records model %q, want the one under test", got)
	}
	if h.count(t, `SELECT count(*) FROM providers`) != 0 {
		t.Fatal("a test saved a provider")
	}
}

func TestAnUnsavedModelIsPricedFromTheTableWhenItIsThere(t *testing.T) {
	h := newBareHarness(t)

	res, err := h.client.Reserve(h.ctx(t), testReq("claude-opus-5-5"))

	if err != nil || res.GetEstMicros() != 20*4+16*20 {
		t.Fatalf("res %v, err %v; want opus's table price on 20 in and 16 out", res, err)
	}
}

func TestAnUnsavedModelWithNoPriceIsHeldAtNoCostButStillCounted(t *testing.T) {
	h := newBareHarness(t)

	res, err := h.client.Reserve(h.ctx(t), testReq("my-llama-70b"))
	entry := h.commit(t, res.GetReservationId(), 12)

	if err != nil || res.GetEstMicros() != 0 || entry.GetCostMicros() != 0 || entry.GetUsage().GetInputTokens() != 12 {
		t.Fatalf("res %v, entry %v, err %v; want 0 cost and 12 tokens recorded", res, entry, err)
	}
}

func TestOnlyTheConnectionTestMayReserveWithoutASavedProvider(t *testing.T) {
	h := newBareHarness(t)
	m := "my-llama-70b"
	req := fudeReq(10, 0)
	req.UnsavedModel = &m

	_, err := h.client.Reserve(h.ctx(t), req)

	requireStatus(t, err, codes.InvalidArgument, "INVALID_REQUEST")
	if h.count(t, `SELECT count(*) FROM reservations`) != 0 {
		t.Fatal("a refused request held something")
	}
}

func TestAnUnsavedModelNameIsChecked(t *testing.T) {
	h := newBareHarness(t)

	_, err := h.client.Reserve(h.ctx(t), testReq("bad model\nname"))

	requireStatus(t, err, codes.InvalidArgument, "INVALID_PROVIDER_MODEL")
}

func TestATestStillCountsAgainstTheHardBudgets(t *testing.T) {
	h := newBareHarness(t)
	h.seedDefaults(t)
	h.setLimit(t, "global", "", 10)

	_, err := h.client.Reserve(h.ctx(t), testReq("claude-opus-5-5"))

	requireStatus(t, err, codes.ResourceExhausted, "BUDGET_EXHAUSTED")
}

func TestASavedProvidersTokenCapStillAppliesToATest(t *testing.T) {
	h := capHarness(t, ptr(30))

	_, err := h.client.Reserve(h.ctx(t), testReq("another-model"))

	requireStatus(t, err, codes.ResourceExhausted, "TOKEN_CAP_EXHAUSTED")
}

func TestATestIsHeldAgainstTheFeatureBudgetToo(t *testing.T) {
	h := newBareHarness(t)

	_, err := h.client.Reserve(h.ctx(t), testReq("claude-opus-5-5"))

	if err != nil || h.reserved(t, "feature", "fude.test_connection") != 20*4+16*20 {
		t.Fatalf("err %v, reserved %d; want the estimate held on the feature budget", err, h.reserved(t, "feature", "fude.test_connection"))
	}
}

func TestATestOnlyHoldsForItsOwnOwner(t *testing.T) {
	h := newBareHarness(t)
	other := "11111111-1111-4111-8111-111111111111"

	res, err := h.client.Reserve(h.ctxFor(t, other), testReq("my-llama-70b"))
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if h.count(t, `SELECT count(*) FROM reservations WHERE owner_id = $1`, other) != 1 ||
		h.count(t, `SELECT count(*) FROM reservations WHERE owner_id = $1`, h.owner) != 0 {
		t.Fatalf("reservation %s landed on the wrong owner", res.GetReservationId())
	}
}
