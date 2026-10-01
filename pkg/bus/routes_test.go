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

func TestConsumersStartsEmpty(t *testing.T) {
	if got := Consumers("job.added"); got != nil {
		t.Fatalf("consumers = %v, want nil while routes are empty", got)
	}
}
