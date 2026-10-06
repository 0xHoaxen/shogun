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

func TestRecipientDigest(t *testing.T) {
	tests := []struct {
		name      string
		a, b      []string
		wantEqual bool
	}{
		{"same list", []string{"a@x.example"}, []string{"a@x.example"}, true},
		{"order does not matter", []string{"a@x.example", "b@x.example"}, []string{"b@x.example", "a@x.example"}, true},
		{"case and space do not matter", []string{"A@X.example "}, []string{"a@x.example"}, true},
		{"duplicates collapse", []string{"a@x.example", "a@x.example"}, []string{"a@x.example"}, true},
		{"different address", []string{"a@x.example"}, []string{"b@x.example"}, false},
		{"an extra recipient", []string{"a@x.example"}, []string{"a@x.example", "b@x.example"}, false},
		{"joined addresses are not one address", []string{"a@x.example", "b@x.example"}, []string{"a@x.exampleb@x.example"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bytes.Equal(RecipientDigest(tt.a), RecipientDigest(tt.b)); got != tt.wantEqual {
				t.Errorf("equal = %v, want %v", got, tt.wantEqual)
			}
		})
	}
	if len(RecipientDigest(nil)) != 32 {
		t.Error("an empty list must still hash to 32 bytes")
	}
}
