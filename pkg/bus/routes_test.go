package bus

import (
	"slices"
	"testing"
)

func TestConsumersInReturnsCopyOfRoute(t *testing.T) {
	table := map[string][]string{"job.added": {"taiko", "sensei"}}

	got := consumersIn(table, "job.added")
	if !slices.Equal(got, []string{"taiko", "sensei"}) {
		t.Fatalf("consumers = %v", got)
	}
	got[0] = "mutated"
	if table["job.added"][0] != "taiko" {
		t.Fatal("Consumers must return a copy of the route")
	}
}

func TestConsumersInUnknownTypeReturnsNil(t *testing.T) {
	if got := consumersIn(map[string][]string{"a.b": {"x"}}, "c.d"); got != nil {
		t.Fatalf("consumers = %v, want nil", got)
	}
}

func TestConsumersRoutesMailEventsToKagami(t *testing.T) {
	for _, eventType := range []string{"mail.classified", "mail.reply_detected", "draft.sent"} {
		if got := Consumers(eventType); !slices.Contains(got, "kagami") {
			t.Errorf("Consumers(%q) = %v, want kagami among them", eventType, got)
		}
	}
}

func TestConsumersRoutesEventsFudeDraftsFor(t *testing.T) {
	for _, eventType := range []string{"job.added", "contact.status_changed", "draft.sent", "draft.send_failed"} {
		if got := Consumers(eventType); !slices.Contains(got, "fude") {
			t.Errorf("Consumers(%q) = %v, want fude among them", eventType, got)
		}
	}
	if got := Consumers("no.such_event"); got != nil {
		t.Errorf("consumers = %v, want nil for an event nobody consumes", got)
	}
}
