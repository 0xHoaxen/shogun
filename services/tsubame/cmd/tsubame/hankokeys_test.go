package main

import (
	"strings"
	"testing"
)

func TestLoadHankoKeys(t *testing.T) {
	good := "A6EHv/POEL4dcN0Y50vAmWfk1jCbpQ1fHdyGZBJVMbg="
	tests := []struct {
		name    string
		env     map[string]string
		wantIDs []string
		wantErr string
	}{
		{"one key", map[string]string{hankoKeysEnv: "fude-1=" + good}, []string{"fude-1"}, ""},
		{"two keys while rotating", map[string]string{hankoKeysEnv: "fude-1=" + good + ", fude-2=" + good}, []string{"fude-1", "fude-2"}, ""},
		{"missing", map[string]string{}, nil, hankoKeysEnv},
		{"a bare key with no id", map[string]string{hankoKeysEnv: good}, nil, "Ed25519"},
		{"no equals sign at all", map[string]string{hankoKeysEnv: "justonething"}, nil, "key-id=base64"},
		{"an empty key id", map[string]string{hankoKeysEnv: "=" + good}, nil, "key-id=base64"},
		{"not base64", map[string]string{hankoKeysEnv: "fude-1=%%%"}, nil, "Ed25519"},
		{"wrong length", map[string]string{hankoKeysEnv: "fude-1=AAEC"}, nil, "Ed25519"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, err := loadHankoKeys(lookupOf(tt.env))

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got %v, want an error mentioning %q", err, tt.wantErr)
				}
				return
			}
			if err != nil || len(keys) != len(tt.wantIDs) {
				t.Fatalf("got %d keys, %v", len(keys), err)
			}
			for _, id := range tt.wantIDs {
				if _, ok := keys[id]; !ok {
					t.Errorf("missing key %q", id)
				}
			}
		})
	}
}

func lookupOf(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
}
