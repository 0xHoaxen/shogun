package live

import (
	"testing"

	"github.com/google/uuid"
)

func TestParsePayload(t *testing.T) {
	owner, id := uuid.New(), uuid.New()
	tests := []struct {
		name    string
		payload string
		wantErr bool
	}{
		{"owner and id", owner.String() + ":" + id.String(), false},
		{"empty", "", true},
		{"no separator", owner.String(), true},
		{"owner is not a uuid", "nope:" + id.String(), true},
		{"id is not a uuid", owner.String() + ":nope", true},
		{"extra part", owner.String() + ":" + id.String() + ":x", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotOwner, gotID, err := parsePayload(tt.payload)

			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr %v, got %v", tt.wantErr, err)
			}
			if !tt.wantErr && (gotOwner != owner || gotID != id) {
				t.Fatalf("got %s and %s", gotOwner, gotID)
			}
		})
	}
}
