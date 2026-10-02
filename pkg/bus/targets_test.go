package bus

import (
	"reflect"
	"testing"
)

func TestConsumersOf(t *testing.T) {
	tests := []struct {
		name  string
		table map[string][]string
		want  []string
	}{
		{name: "empty table has no consumers", table: map[string][]string{}, want: []string{}},
		{
			name: "deduplicates and sorts across event types",
			table: map[string][]string{
				"job.added":    {"taiko", "fude"},
				"draft.sent":   {"kagami", "taiko"},
				"cost.crossed": nil,
			},
			want: []string{"fude", "kagami", "taiko"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got := consumersOf(tc.table)

			// Assert
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("consumersOf = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestTargets(t *testing.T) {
	env := map[string]string{
		"TAIKO_ADDR":  "taiko:9090",
		"FUDE_ADDR":   "",
		"OTHER_ADDR":  "other:9090",
		"KAGAMI_ADDR": "kagami:9090",
	}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }

	tests := []struct {
		name      string
		consumers []string
		want      map[string]string
	}{
		{name: "no consumers gives empty map", consumers: nil, want: map[string]string{}},
		{
			name:      "reads upper-cased NAME_ADDR for each consumer",
			consumers: []string{"taiko", "kagami"},
			want:      map[string]string{"taiko": "taiko:9090", "kagami": "kagami:9090"},
		},
		{
			name:      "leaves out consumers with unset or empty address",
			consumers: []string{"taiko", "fude", "sensei"},
			want:      map[string]string{"taiko": "taiko:9090"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Act
			got := Targets(lookup, tc.consumers)

			// Assert
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Targets = %v, want %v", got, tc.want)
			}
		})
	}
}
