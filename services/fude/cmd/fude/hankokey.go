package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"

	"github.com/0xHoaxen/shogun/pkg/config"
)

const (
	hankoKeyEnv   = "FUDE_HANKO_SIGNING_KEY"
	hankoKeyIDEnv = "FUDE_HANKO_KEY_ID"
	hankoKeyIDDef = "fude-1"
)

// loadHankoKey reads the Hanko signing key: a base64 Ed25519 seed of 32 bytes
// (make one with `openssl rand -base64 32`). It is required, so a fude without
// a key does not start. The key itself never appears in an error.
func loadHankoKey(lookup config.LookupFunc) (ed25519.PrivateKey, string, error) {
	raw, err := config.Required(lookup, hankoKeyEnv)
	if err != nil {
		return nil, "", err
	}
	seed, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, "", fmt.Errorf("%s is not base64", hankoKeyEnv)
	}
	if len(seed) != ed25519.SeedSize {
		return nil, "", fmt.Errorf("%s must decode to %d bytes, got %d", hankoKeyEnv, ed25519.SeedSize, len(seed))
	}
	return ed25519.NewKeyFromSeed(seed), config.String(lookup, hankoKeyIDEnv, hankoKeyIDDef), nil
}
