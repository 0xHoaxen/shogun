package envelope

import (
	"encoding/base64"
	"fmt"

	"github.com/0xHoaxen/shogun/pkg/config"
)

// KeyringFromEnv builds a Keyring from one master key in the environment: keyEnv
// holds 32 bytes as base64 (make one with `openssl rand -base64 32`) and idEnv
// names it, falling back to defaultID. The key is required, so a service that
// seals secrets does not start without it, and it never appears in an error.
func KeyringFromEnv(lookup config.LookupFunc, keyEnv, idEnv, defaultID string) (*Keyring, error) {
	raw, err := config.Required(lookup, keyEnv)
	if err != nil {
		return nil, err
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%s is not base64", keyEnv)
	}
	if len(key) != KeySize {
		return nil, fmt.Errorf("%s must decode to %d bytes, got %d", keyEnv, KeySize, len(key))
	}
	id := config.String(lookup, idEnv, defaultID)
	return NewKeyring(id, map[string][]byte{id: key})
}
