package envelope_test

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/0xHoaxen/shogun/pkg/envelope"
)

func lookupOf(env map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
}

func TestKeyringFromEnvReadsTheMasterKeyAndItsID(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", envelope.KeySize)))

	ring, err := envelope.KeyringFromEnv(lookupOf(map[string]string{"X_KEY": key, "X_KEY_ID": "x-2"}), "X_KEY", "X_KEY_ID", "x-1")

	if err != nil || ring.CurrentID() != "x-2" {
		t.Fatalf("ring %v, err %v; want the key sealed under id x-2", ring, err)
	}
	sealed, id, err := ring.Seal([]byte("secret"), []byte("aad"))
	if err != nil || id != "x-2" {
		t.Fatalf("Seal: %v, %v", id, err)
	}
	if opened, err := ring.Open(sealed, id, []byte("aad")); err != nil || string(opened) != "secret" {
		t.Fatalf("Open = %q, %v", opened, err)
	}
}

func TestKeyringFromEnvUsesTheDefaultIDWhenNoneIsSet(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", envelope.KeySize)))

	ring, err := envelope.KeyringFromEnv(lookupOf(map[string]string{"X_KEY": key}), "X_KEY", "X_KEY_ID", "x-1")

	if err != nil || ring.CurrentID() != "x-1" {
		t.Fatalf("ring %v, err %v", ring, err)
	}
}

func TestKeyringFromEnvRefusesAMissingOrBadKeyWithoutRepeatingIt(t *testing.T) {
	short := base64.StdEncoding.EncodeToString([]byte("too short"))
	tests := map[string]map[string]string{
		"missing":    {},
		"not base64": {"X_KEY": "!!!not-base64!!!"},
		"too short":  {"X_KEY": short},
	}
	for name, env := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := envelope.KeyringFromEnv(lookupOf(env), "X_KEY", "X_KEY_ID", "x-1")

			if err == nil {
				t.Fatal("KeyringFromEnv accepted it")
			}
			if v := env["X_KEY"]; v != "" && strings.Contains(err.Error(), v) {
				t.Fatalf("the error repeats the key: %v", err)
			}
		})
	}
}
