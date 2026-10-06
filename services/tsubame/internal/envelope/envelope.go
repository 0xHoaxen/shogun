// Package envelope seals secrets with envelope encryption: each value gets its
// own random data key, and the data key is wrapped by a master key. Master keys
// carry an id, so a new one can be rolled in while rows sealed under an older
// one stay readable.
package envelope

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"slices"
)

const (
	// KeySize is the size of a master key and of each data key.
	KeySize = 32

	version     = 1
	nonceSize   = 12
	tagSize     = 16
	wrappedSize = KeySize + tagSize
	headerSize  = 1 + nonceSize + wrappedSize + nonceSize
)

// Errors Open returns. They never say more than that opening failed, so they
// cannot be used to probe the keys.
var (
	// ErrUnknownKey means the master key id is not in the keyring.
	ErrUnknownKey = errors.New("envelope: unknown master key id")
	// ErrCorrupt means the value is damaged, was sealed for another context, or
	// was sealed under a different key.
	ErrCorrupt = errors.New("envelope: value cannot be opened")
)

// Keyring holds the master keys. Seal uses the current one; Open uses
// whichever the value names.
type Keyring struct {
	current string
	keys    map[string][]byte
}

// NewKeyring returns a Keyring that seals with the key named current. keys maps
// every key id still in use to its 32 bytes; it is copied.
func NewKeyring(current string, keys map[string][]byte) (*Keyring, error) {
	if _, ok := keys[current]; !ok {
		return nil, fmt.Errorf("envelope: current key id %q is not in the keyring", current)
	}
	copied := make(map[string][]byte, len(keys))
	for id, key := range keys {
		if id == "" {
			return nil, errors.New("envelope: a key id is empty")
		}
		if len(key) != KeySize {
			return nil, fmt.Errorf("envelope: key %q has %d bytes, want %d", id, len(key), KeySize)
		}
		copied[id] = slices.Clone(key)
	}
	return &Keyring{current: current, keys: copied}, nil
}

// CurrentID is the id of the key Seal uses.
func (k *Keyring) CurrentID() string { return k.current }

// Seal encrypts plaintext under a fresh data key wrapped by the current master
// key, and returns the sealed value and the master key id to store with it.
// aad is bound into the value: Open succeeds only with the same aad, so a value
// cannot be moved to another row.
func (k *Keyring) Seal(plaintext, aad []byte) (sealed []byte, keyID string, err error) {
	dek := make([]byte, KeySize)
	if _, err := rand.Read(dek); err != nil {
		return nil, "", fmt.Errorf("envelope: data key: %w", err)
	}
	wrapNonce, wrapped, err := encrypt(k.keys[k.current], dek, aad)
	if err != nil {
		return nil, "", err
	}
	dataNonce, body, err := encrypt(dek, plaintext, aad)
	if err != nil {
		return nil, "", err
	}
	sealed = make([]byte, 0, headerSize+len(body))
	sealed = append(sealed, version)
	sealed = append(sealed, wrapNonce...)
	sealed = append(sealed, wrapped...)
	sealed = append(sealed, dataNonce...)
	sealed = append(sealed, body...)
	return sealed, k.current, nil
}

// Open decrypts a value sealed with Seal under the master key keyID and the
// same aad.
func (k *Keyring) Open(sealed []byte, keyID string, aad []byte) ([]byte, error) {
	master, ok := k.keys[keyID]
	if !ok {
		return nil, ErrUnknownKey
	}
	if len(sealed) < headerSize+tagSize || sealed[0] != version {
		return nil, ErrCorrupt
	}
	rest := sealed[1:]
	wrapNonce, rest := rest[:nonceSize], rest[nonceSize:]
	wrapped, rest := rest[:wrappedSize], rest[wrappedSize:]
	dataNonce, body := rest[:nonceSize], rest[nonceSize:]

	dek, err := decrypt(master, wrapNonce, wrapped, aad)
	if err != nil {
		return nil, ErrCorrupt
	}
	plaintext, err := decrypt(dek, dataNonce, body, aad)
	if err != nil {
		return nil, ErrCorrupt
	}
	return plaintext, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("envelope: cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

func encrypt(key, plaintext, aad []byte) (nonce, ciphertext []byte, err error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, nil, fmt.Errorf("envelope: nonce: %w", err)
	}
	return nonce, gcm.Seal(nil, nonce, plaintext, aad), nil
}

func decrypt(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, aad)
}
