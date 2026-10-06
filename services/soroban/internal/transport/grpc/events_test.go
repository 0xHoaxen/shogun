package grpc_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
)

// payloadOf decodes the payload of the only outbox event of a type.
func (h *harness) payloadOf(t *testing.T, eventType string, into proto.Message) {
	t.Helper()
	var raw []byte
	if err := h.pool.QueryRow(context.Background(), `SELECT payload FROM outbox WHERE type = $1`, eventType).Scan(&raw); err != nil {
		t.Fatalf("read %s: %v", eventType, err)
	}
	var env eventsv1.Envelope
	if err := proto.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if err := env.GetPayload().UnmarshalTo(into); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
}

func TestBudgetExhaustedEventNamesTheOwner(t *testing.T) {
	h := newHarness(t)
	h.seedDefaults(t)
	h.setLimit(t, "global", "", 1000)

	_, err := h.client.Reserve(h.ctx(t), reserveReq(1000))
	requireStatus(t, err, codes.ResourceExhausted, "BUDGET_EXHAUSTED")
	var got sorobanv1.CostBudgetExhausted
	h.payloadOf(t, "cost.budget_exhausted", &got)

	if got.GetOwnerId() != h.owner {
		t.Fatalf("got owner %q, want %q", got.GetOwnerId(), h.owner)
	}
}

func TestThresholdReachedEventNamesTheOwner(t *testing.T) {
	h := newHarness(t)
	h.seedDefaults(t)
	h.setLimit(t, "global", "", 1_000_000)
	h.setLimit(t, "service", "fude", 100_000_000)
	h.setLimit(t, "feature", "fude.cover_letter", 100_000_000)
	const tokens500k = 125_000 // 4 micros per token

	h.commit(t, h.reserve(t, tokens500k), tokens500k)
	var got sorobanv1.CostThresholdReached
	h.payloadOf(t, "cost.threshold_reached", &got)

	if got.GetOwnerId() != h.owner {
		t.Fatalf("got owner %q, want %q", got.GetOwnerId(), h.owner)
	}
}
