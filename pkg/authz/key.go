package authz

import (
	"fmt"

	"github.com/0xHoaxen/shogun/pkg/config"
)

// KeyEnv is the environment variable holding the identity signing key.
const KeyEnv = "IDENTITY_SIGNING_KEY"

// LoadKey reads the signing key from the environment and rejects short keys.
func LoadKey(lookup config.LookupFunc) ([]byte, error) {
	v, err := config.Required(lookup, KeyEnv)
	if err != nil {
		return nil, fmt.Errorf("authz: %w", err)
	}
	if len(v) < MinKeyLength {
		return nil, fmt.Errorf("%w: %s needs at least %d bytes", ErrShortKey, KeyEnv, MinKeyLength)
	}
	return []byte(v), nil
}
