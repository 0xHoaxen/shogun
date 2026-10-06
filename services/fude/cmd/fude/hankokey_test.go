package main

import (
	"crypto/ed25519"
	"strings"
	"testing"
)

func lookupOf(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
}

func TestLoadHankoKey(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
		wantKID string
	}{
		{"valid with the default key id", map[string]string{hankoKeyEnv: testHankoKey}, "", hankoKeyIDDef},
		{"valid with a chosen key id", map[string]string{hankoKeyEnv: testHankoKey, hankoKeyIDEnv: "fude-2"}, "", "fude-2"},
		{"missing", map[string]string{}, hankoKeyEnv, ""},
		{"not base64", map[string]string{hankoKeyEnv: "%%%"}, "not base64", ""},
		{"wrong length", map[string]string{hankoKeyEnv: "AAEC"}, "must decode to 32 bytes", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			key, kid, err := loadHankoKey(lookupOf(tt.env))

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("got %v, want an error mentioning %q", err, tt.wantErr)
				}
				if err != nil && strings.Contains(err.Error(), tt.env[hankoKeyEnv]) && tt.env[hankoKeyEnv] != "" {
					t.Fatalf("error leaks the key value: %v", err)
				}
				return
			}
			if err != nil || len(key) != ed25519.PrivateKeySize || kid != tt.wantKID {
				t.Fatalf("got %d-byte key, kid %q, err %v", len(key), kid, err)
			}
		})
	}
}

func TestRunRefusesToStartWithoutAHankoKey(t *testing.T) {
	env := map[string]string{
		"ENVIRONMENT": "test", "DATABASE_URL": "postgres://unused", "IDENTITY_SIGNING_KEY": testIdentityKey,
	}

	err := run(t.Context(), lookupOf(env), relayOverrides{})

	if err == nil || !strings.Contains(err.Error(), hankoKeyEnv) {
		t.Fatalf("got %v, want a refusal naming %s", err, hankoKeyEnv)
	}
}
