package grpc_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/services/soroban/internal/app"
)

// capHarness is a harness whose owner uses a model nobody has a price for, with
// a monthly token cap (nil means none), which is the case the cap exists for.
func capHarness(t *testing.T, capTokens *int64) *harness {
	t.Helper()
	h := newBareHarness(t)
	if _, err := h.svc.SetProvider(appCtx(h.owner), app.ProviderInput{
		BaseURL: "https://llm.example.com", Model: "my-llama-70b", APIKey: secretKey, MonthlyTokenCap: capTokens,
	}); err != nil {
		t.Fatalf("SetProvider: %v", err)
	}
	return h
}

func ptr(n int64) *int64 { return &n }

func TestWithoutACapSpendingIsUnlimited(t *testing.T) {
	h := capHarness(t, nil)

	res, err := h.client.Reserve(h.ctx(t), fudeReq(50_000_000, 10_000_000))

	if err != nil || res.GetEstMicros() != 0 {
		t.Fatalf("res %v, err %v; want an unpriced model with no cap allowed whatever the size", res, err)
	}
}

func TestACapRefusesACallThatWouldPassIt(t *testing.T) {
	// Arrange
	h := capHarness(t, ptr(10_000))
	first, err := h.client.Reserve(h.ctx(t), fudeReq(4000, 0))
	if err != nil {
		t.Fatalf("first Reserve: %v", err)
	}
	h.commit(t, first.GetReservationId(), 4000)

	// Act
	_, tooBig := h.client.Reserve(h.ctx(t), fudeReq(7000, 0))
	_, fits := h.client.Reserve(h.ctx(t), fudeReq(5000, 0))

	// Assert
	requireStatus(t, tooBig, codes.ResourceExhausted, "TOKEN_CAP_EXHAUSTED")
	if fits != nil {
		t.Fatalf("a call that fits under the cap was refused: %v", fits)
	}
}

func TestCallsStillInFlightCountAgainstTheCap(t *testing.T) {
	h := capHarness(t, ptr(10_000))
	first, err := h.client.Reserve(h.ctx(t), fudeReq(6000, 0))
	if err != nil {
		t.Fatalf("first Reserve: %v", err)
	}

	_, second := h.client.Reserve(h.ctx(t), fudeReq(6000, 0))
	_, release := h.client.Release(h.ctx(t), &sorobanv1.ReleaseRequest{ReservationId: first.GetReservationId()})
	_, third := h.client.Reserve(h.ctx(t), fudeReq(6000, 0))

	requireStatus(t, second, codes.ResourceExhausted, "TOKEN_CAP_EXHAUSTED")
	if release != nil || third != nil {
		t.Fatalf("release %v, reserve after release %v; want the released tokens usable again", release, third)
	}
}

func TestOutputTokensCountTowardsTheCapToo(t *testing.T) {
	h := capHarness(t, ptr(10_000))

	_, err := h.client.Reserve(h.ctx(t), fudeReq(1000, 9500))

	requireStatus(t, err, codes.ResourceExhausted, "TOKEN_CAP_EXHAUSTED")
}

func TestARefusedCapWritesNothingAndRaisesNoBudgetEvent(t *testing.T) {
	h := capHarness(t, ptr(1000))

	_, err := h.client.Reserve(h.ctx(t), fudeReq(5000, 0))

	requireStatus(t, err, codes.ResourceExhausted, "TOKEN_CAP_EXHAUSTED")
	var reservations int
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM reservations`).Scan(&reservations)
	if reservations != 0 || h.outboxCount(t, "") != 0 {
		t.Fatalf("%d reservations and %d events written for a refused call", reservations, h.outboxCount(t, ""))
	}
}

func TestTheRefusalSaysWhenTheMonthEnds(t *testing.T) {
	h := capHarness(t, ptr(1000))

	_, err := h.client.Reserve(h.ctx(t), fudeReq(5000, 0))

	info := errorInfo(err)
	if info == nil || info.GetMetadata()["resets_at"] != "2026-10-31T18:30:00Z" {
		t.Fatalf("info = %v, want resets_at at the start of November in India time", info)
	}
}

func TestTheCapAppliesToAModelWithAPriceToo(t *testing.T) {
	h := newBareHarness(t)
	if _, err := h.svc.SetProvider(appCtx(h.owner), app.ProviderInput{
		BaseURL: "https://llm.example.com", Model: "claude-opus-5-5", APIKey: secretKey, MonthlyTokenCap: ptr(500),
	}); err != nil {
		t.Fatalf("SetProvider: %v", err)
	}

	_, err := h.client.Reserve(h.ctx(t), fudeReq(600, 0))

	requireStatus(t, err, codes.ResourceExhausted, "TOKEN_CAP_EXHAUSTED")
}

// seedUsage writes a committed call of tokens input tokens for owner at the given time.
func (h *harness) seedUsage(t *testing.T, owner string, tokens int64, at time.Time) {
	t.Helper()
	ctx := context.Background()
	var id string
	if err := h.pool.QueryRow(ctx, `
		INSERT INTO reservations (id, owner_id, service, feature, model, est_micros, budget_period_ids, created_at, expires_at, status)
		VALUES (gen_random_uuid(), $1, 'fude', 'fude.post', 'm', 0, '{}', $2, $2, 'committed') RETURNING id::text`,
		owner, at).Scan(&id); err != nil {
		t.Fatalf("seed reservation: %v", err)
	}
	if _, err := h.pool.Exec(ctx, `
		INSERT INTO ledger (id, owner_id, reservation_id, service, feature, model, input_tokens, output_tokens,
		                    cache_read_tokens, cache_write_tokens, cost_micros, occurred_at)
		VALUES (gen_random_uuid(), $1, $2::uuid, 'fude', 'fude.post', 'm', $3, 0, 0, 0, 0, $4)`, owner, id, tokens, at); err != nil {
		t.Fatalf("seed ledger: %v", err)
	}
}

func TestLastMonthsAndOtherUsersTokensDoNotCount(t *testing.T) {
	// Arrange: a huge entry from September for this user, and another user's from October.
	h := capHarness(t, ptr(10_000))
	other := uuid.NewString()
	h.useProvider(t, other, "my-llama-70b")
	h.seedUsage(t, h.owner, 1_000_000, time.Date(2026, 9, 20, 12, 0, 0, 0, time.UTC))
	h.seedUsage(t, other, 1_000_000, fixedNow)

	// Act
	_, err := h.client.Reserve(h.ctx(t), fudeReq(9000, 0))
	// Assert
	if err != nil {
		t.Fatalf("Reserve: %v; tokens from last month or another user were counted", err)
	}
}

func TestTokensUsedEarlierThisMonthCount(t *testing.T) {
	h := capHarness(t, ptr(10_000))
	h.seedUsage(t, h.owner, 9_500, fixedNow.Add(-24*time.Hour))

	_, err := h.client.Reserve(h.ctx(t), fudeReq(1000, 0))

	requireStatus(t, err, codes.ResourceExhausted, "TOKEN_CAP_EXHAUSTED")
}

func TestACapHoldsUnderConcurrentReservations(t *testing.T) {
	// Arrange: twenty calls of 1000 tokens against a 10000 cap.
	h := capHarness(t, ptr(10_000))
	var ok, refused atomic.Int32
	var wg sync.WaitGroup

	// Act
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := h.client.Reserve(h.ctx(t), fudeReq(1000, 0))
			if err == nil {
				ok.Add(1)
				return
			}
			if info := errorInfo(err); info != nil && info.GetReason() == "TOKEN_CAP_EXHAUSTED" {
				refused.Add(1)
			}
		}()
	}
	wg.Wait()

	// Assert
	if ok.Load() != 10 || refused.Load() != 10 {
		t.Fatalf("%d allowed and %d refused, want exactly 10 and 10: the cap must not be passed", ok.Load(), refused.Load())
	}
}

func TestTheCapIsSetThroughSetProviderAndCanBeRemoved(t *testing.T) {
	h := newBareHarness(t)
	req := goodProvider()
	req.MonthlyTokenCap = ptr(250_000)
	set := h.setProvider(t, h.owner, req)

	req.Version, req.ApiKey = set.GetVersion(), ""
	req.MonthlyTokenCap = nil
	cleared := h.setProvider(t, h.owner, req)

	if set.MonthlyTokenCap == nil || *set.MonthlyTokenCap != 250_000 {
		t.Fatalf("provider = %v, want the cap back", set)
	}
	if cleared.MonthlyTokenCap != nil {
		t.Fatalf("cap = %v after an update without one, want it removed", *cleared.MonthlyTokenCap)
	}
}

func TestSetProviderRefusesACapThatIsNotAboveZero(t *testing.T) {
	for _, bad := range []int64{0, -5} {
		h := newBareHarness(t)
		req := goodProvider()
		req.MonthlyTokenCap = ptr(bad)

		_, err := h.client.SetProvider(h.ctx(t), req)

		requireStatus(t, err, codes.InvalidArgument, "INVALID_TOKEN_CAP")
	}
}

func TestGetProviderShowsTheTokensUsedThisMonth(t *testing.T) {
	h := capHarness(t, ptr(100_000))
	res, err := h.client.Reserve(h.ctx(t), fudeReq(3000, 500))
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	h.commit(t, res.GetReservationId(), 2500)

	got, err := h.client.GetProvider(h.ctx(t), &sorobanv1.GetProviderRequest{})

	if err != nil || got.GetTokensThisMonth() != 2500 {
		t.Fatalf("got %v, %v; want the 2500 committed tokens", got, err)
	}
}

func TestUsersEditOnlyTheirOwnBudgets(t *testing.T) {
	// Arrange: two users, each with the default budgets.
	h := newHarness(t)
	other := uuid.NewString()
	h.useProvider(t, other, "claude-opus-5-5")
	if _, err := h.client.Reserve(h.ctxFor(t, other), fudeReq(10, 0)); err != nil {
		t.Fatalf("Reserve as the other user: %v", err)
	}
	if _, err := h.client.Reserve(h.ctx(t), fudeReq(10, 0)); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	theirs, err := h.client.ListBudgets(h.ctxFor(t, other), &sorobanv1.ListBudgetsRequest{})
	if err != nil || len(theirs.GetBudgets()) == 0 {
		t.Fatalf("ListBudgets: %v %v", theirs, err)
	}
	target := theirs.GetBudgets()[0]

	// Act: this user tries to change the other user's budget by id.
	_, edit := h.client.SetBudget(h.ctx(t), &sorobanv1.SetBudgetRequest{
		Id: target.GetId(), LimitMicros: 1, Mode: target.GetMode(), Enabled: true, Version: target.GetVersion(),
	})

	// Assert
	requireStatus(t, edit, codes.NotFound, "RESOURCE_NOT_FOUND")
	after, _ := h.client.ListBudgets(h.ctxFor(t, other), &sorobanv1.ListBudgetsRequest{})
	for _, b := range after.GetBudgets() {
		if b.GetId() == target.GetId() && b.GetLimitMicros() != target.GetLimitMicros() {
			t.Fatalf("another user's budget limit changed to %d", b.GetLimitMicros())
		}
	}
}
