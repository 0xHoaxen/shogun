package hanko

import (
	"bytes"
	"testing"
)

func TestBodyDigest(t *testing.T) {
	tests := []struct {
		name         string
		subjA, bodyA string
		subjB, bodyB string
		wantEqual    bool
	}{
		{"same input", "Hi", "Body", "Hi", "Body", true},
		{"different body", "Hi", "Body", "Hi", "Body!", false},
		{"text moved from subject to body", "ab", "c", "a", "bc", false},
		{"empty subject differs from empty body", "x", "", "", "x", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := BodyDigest(tt.subjA, tt.bodyA)
			b := BodyDigest(tt.subjB, tt.bodyB)

			if got := bytes.Equal(a, b); got != tt.wantEqual {
				t.Errorf("equal = %v, want %v", got, tt.wantEqual)
			}
			if len(a) != 32 {
				t.Errorf("len = %d, want 32", len(a))
			}
		})
	}
}
