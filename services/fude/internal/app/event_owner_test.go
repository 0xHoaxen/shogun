package app_test

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
)

// payload decodes the payload of the only outbox event of a type.
func (e *env) payload(t *testing.T, eventType string, into proto.Message) {
	t.Helper()
	var raw []byte
	if err := e.pool.QueryRow(context.Background(), `SELECT payload FROM outbox WHERE type = $1`, eventType).Scan(&raw); err != nil {
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

func TestDraftReadyEventNamesTheOwner(t *testing.T) {
	e := newEnv(t, fakeSource{target: app.TargetContext{Summary: "Backend role at Lumen"}})
	d := e.newDraft(t, "cover_letter", "email")

	if err := e.gen.Generate(context.Background(), e.args(d, 1)); err != nil {
		t.Fatalf("generate: %v", err)
	}
	var got fudev1.DraftReady
	e.payload(t, "draft.ready", &got)

	if got.GetOwnerId() != e.owner.String() || got.GetDraftId() != d.ID.String() {
		t.Fatalf("got %+v, want owner %s and draft %s", &got, e.owner, d.ID)
	}
}

func TestDraftFailedEventNamesTheOwner(t *testing.T) {
	e := newEnv(t, fakeSource{})
	d := e.newDraft(t, "cover_letter", "email")

	if err := e.gen.Fail(context.Background(), e.args(d, 1), app.ReasonGenerationFailed); err != nil {
		t.Fatalf("fail: %v", err)
	}
	var got fudev1.DraftFailed
	e.payload(t, "draft.failed", &got)

	if got.GetOwnerId() != e.owner.String() || got.GetDraftId() != d.ID.String() {
		t.Fatalf("got %+v, want owner %s and draft %s", &got, e.owner, d.ID)
	}
}
