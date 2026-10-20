package grpc_test

import (
	"context"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/services/soroban/internal/app"
)

func fudeReq(in, out int32) *sorobanv1.ReserveRequest {
	return &sorobanv1.ReserveRequest{Service: "fude", Feature: "fude.cover_letter", EstInputTokens: in, EstOutputTokens: out}
}

func (h *harness) reservationModel(t *testing.T, id string) string {
	t.Helper()
	var model string
	if err := h.pool.QueryRow(context.Background(), `SELECT model FROM reservations WHERE id = $1`, id).Scan(&model); err != nil {
		t.Fatalf("reservation model: %v", err)
	}
	return model
}

func TestReserveHandsBackTheCallersProviderForTheCall(t *testing.T) {
	h := newHarness(t)

	res, err := h.client.Reserve(h.ctx(t), fudeReq(1000, 0))
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	p := res.GetProvider()
	if p.GetBaseUrl() != "https://llm.example.com/v1" || p.GetModel() != "claude-opus-5-5" || p.GetApiKey() != secretKey {
		t.Fatalf("provider = %v, want the caller's address, model and key", p)
	}
}

func TestReserveUsesTheProvidersModelNotTheOneTheCallerSends(t *testing.T) {
	h := newHarness(t)
	req := fudeReq(1000, 0)
	req.Model = "claude-haiku-4-5" //nolint:staticcheck // the deprecated field is what is under test

	res, err := h.client.Reserve(h.ctx(t), req)

	if err != nil || res.GetProvider().GetModel() != "claude-opus-5-5" || res.GetEstMicros() != 1000*4 {
		t.Fatalf("res %v, err %v; want the provider's model, priced as opus", res, err)
	}
	if got := h.reservationModel(t, res.GetReservationId()); got != "claude-opus-5-5" {
		t.Fatalf("the reservation records model %q, want the provider's", got)
	}
}

func TestReserveWithoutAProviderIsRefusedAndHoldsNothing(t *testing.T) {
	h := newBareHarness(t)

	_, err := h.client.Reserve(h.ctx(t), fudeReq(1000, 0))

	requireStatus(t, err, codes.FailedPrecondition, "AI_PROVIDER_NOT_CONFIGURED")
	var reservations, budgets int
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM reservations`).Scan(&reservations)
	_ = h.pool.QueryRow(context.Background(), `SELECT count(*) FROM budgets WHERE owner_id = $1`, h.owner).Scan(&budgets)
	if reservations != 0 || budgets != 0 {
		t.Fatalf("%d reservations and %d budgets were written for a user with no provider", reservations, budgets)
	}
}

func TestReserveNeverShowsAnotherUsersProvider(t *testing.T) {
	h := newHarness(t)
	other := "11111111-1111-4111-8111-111111111111"

	_, err := h.client.Reserve(h.ctxFor(t, other), fudeReq(10, 0))

	requireStatus(t, err, codes.FailedPrecondition, "AI_PROVIDER_NOT_CONFIGURED")
}

func TestAModelWithNoPriceIsReservedAtNoCostButStillMetered(t *testing.T) {
	h := newBareHarness(t)
	h.useProvider(t, h.owner, "my-llama-70b")

	res, err := h.client.Reserve(h.ctx(t), fudeReq(5000, 1000))

	if err != nil || res.GetEstMicros() != 0 || res.GetProvider().GetModel() != "my-llama-70b" {
		t.Fatalf("res %v, err %v; want the call allowed at an estimate of 0", res, err)
	}
	entry := h.commit(t, res.GetReservationId(), 4200)
	if entry.GetCostMicros() != 0 || entry.GetUsage().GetInputTokens() != 4200 {
		t.Fatalf("entry = %v, want cost 0 and the 4200 tokens recorded", entry)
	}
}

func TestAPriceTheUserEnteredIsUsedForItsModel(t *testing.T) {
	h := newBareHarness(t)
	in, out := int64(2_000_000), int64(8_000_000) // 2 and 8 micro-dollars per token
	if _, err := h.svc.SetProvider(appCtx(h.owner), app.ProviderInput{
		BaseURL: "https://llm.example.com", Model: "my-llama-70b", APIKey: secretKey, InputMicrosPerMTok: &in, OutputMicrosPerMTok: &out,
	}); err != nil {
		t.Fatalf("SetProvider: %v", err)
	}

	res, err := h.client.Reserve(h.ctx(t), fudeReq(1000, 500))
	entry := h.commit(t, res.GetReservationId(), 1000)

	if err != nil || res.GetEstMicros() != 1000*2+500*8 {
		t.Fatalf("res %v, err %v; want 6000 micro-dollars", res, err)
	}
	if entry.GetCostMicros() != 1000*2 {
		t.Fatalf("committed cost = %d, want the entered price applied to the real usage", entry.GetCostMicros())
	}
}

func TestAPriceTheUserEnteredBeatsTheTablesForTheSameModel(t *testing.T) {
	h := newBareHarness(t)
	in, out := int64(40_000_000), int64(80_000_000) // ten times opus's 4 per input token
	if _, err := h.svc.SetProvider(appCtx(h.owner), app.ProviderInput{
		BaseURL: "https://proxy.example.com", Model: "claude-opus-5-5", APIKey: secretKey, InputMicrosPerMTok: &in, OutputMicrosPerMTok: &out,
	}); err != nil {
		t.Fatalf("SetProvider: %v", err)
	}

	res, err := h.client.Reserve(h.ctx(t), fudeReq(1000, 0))

	if err != nil || res.GetEstMicros() != 1000*40 {
		t.Fatalf("res %v, err %v; want the user's price, 40 per token", res, err)
	}
}

func TestReserveSaysSoWhenThisServerCannotOpenKeys(t *testing.T) {
	h := newBareHarness(t)
	h.useProvider(t, h.owner, "claude-opus-5-5")
	noKeys := app.NewService(h.pool, nil)

	_, err := noKeys.Reserve(appCtx(h.owner), app.ReserveInput{Service: "fude", Feature: "fude.cover_letter", EstInputTokens: 10})

	if err == nil || strings.Contains(err.Error(), secretKey) {
		t.Fatalf("err = %v, want a refusal that does not repeat the key", err)
	}
}
