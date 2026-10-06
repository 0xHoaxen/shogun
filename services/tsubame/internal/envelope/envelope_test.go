package envelope

import (
	"bytes"
	"errors"
	"testing"
)

func key(b byte) []byte { return bytes.Repeat([]byte{b}, KeySize) }

func newRing(t *testing.T, current string, keys map[string][]byte) *Keyring {
	t.Helper()
	r, err := NewKeyring(current, keys)
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}
	return r
}

func TestSealThenOpenRoundTrips(t *testing.T) {
	ring := newRing(t, "k1", map[string][]byte{"k1": key(1)})
	secret := []byte("1//refresh-token-value")

	sealed, id, err := ring.Seal(secret, []byte("account-1"))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	got, err := ring.Open(sealed, id, []byte("account-1"))

	if err != nil || !bytes.Equal(got, secret) || id != "k1" {
		t.Fatalf("got %q, id %q, err %v", got, id, err)
	}
	if bytes.Contains(sealed, secret) {
		t.Fatal("the sealed value contains the plaintext")
	}
}

func TestSealingTwiceGivesDifferentValues(t *testing.T) {
	ring := newRing(t, "k1", map[string][]byte{"k1": key(1)})

	a, _, _ := ring.Seal([]byte("same"), nil)
	b, _, _ := ring.Seal([]byte("same"), nil)

	if bytes.Equal(a, b) {
		t.Fatal("two seals of one value are identical, so the data key or nonce is reused")
	}
}

func TestOpenRefusesWhatIsNotExactlyWhatWasSealed(t *testing.T) {
	ring := newRing(t, "k1", map[string][]byte{"k1": key(1), "k2": key(2)})
	sealed, _, _ := ring.Seal([]byte("secret"), []byte("row-1"))
	flip := func(i int) []byte {
		c := bytes.Clone(sealed)
		c[i] ^= 0xff
		return c
	}
	tests := []struct {
		name   string
		sealed []byte
		keyID  string
		aad    []byte
		want   error
	}{
		{"other row", sealed, "k1", []byte("row-2"), ErrCorrupt},
		{"no aad", sealed, "k1", nil, ErrCorrupt},
		{"other master key", sealed, "k2", []byte("row-1"), ErrCorrupt},
		{"unknown key id", sealed, "k9", []byte("row-1"), ErrUnknownKey},
		{"flipped version byte", flip(0), "k1", []byte("row-1"), ErrCorrupt},
		{"flipped wrapped key", flip(1 + nonceSize + 3), "k1", []byte("row-1"), ErrCorrupt},
		{"flipped body", flip(len(sealed) - 1), "k1", []byte("row-1"), ErrCorrupt},
		{"truncated", sealed[:headerSize], "k1", []byte("row-1"), ErrCorrupt},
		{"empty", nil, "k1", []byte("row-1"), ErrCorrupt},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ring.Open(tt.sealed, tt.keyID, tt.aad)

			if !errors.Is(err, tt.want) || got != nil {
				t.Fatalf("got %q, %v; want %v", got, err, tt.want)
			}
		})
	}
}

func TestRotationKeepsOldValuesReadable(t *testing.T) {
	old := newRing(t, "k1", map[string][]byte{"k1": key(1)})
	sealed, oldID, _ := old.Seal([]byte("token"), []byte("a"))
	rotated := newRing(t, "k2", map[string][]byte{"k1": key(1), "k2": key(2)})

	got, err := rotated.Open(sealed, oldID, []byte("a"))
	fresh, freshID, _ := rotated.Seal([]byte("token"), []byte("a"))

	if err != nil || string(got) != "token" {
		t.Fatalf("old value: %q, %v", got, err)
	}
	if freshID != "k2" || rotated.CurrentID() != "k2" {
		t.Fatalf("new values must use the current key, got %q", freshID)
	}
	if _, err := old.Open(fresh, freshID, []byte("a")); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("a ring without k2 opened a k2 value: %v", err)
	}
}

func TestNewKeyringValidates(t *testing.T) {
	tests := []struct {
		name    string
		current string
		keys    map[string][]byte
	}{
		{"current missing", "k1", map[string][]byte{"k2": key(2)}},
		{"short key", "k1", map[string][]byte{"k1": []byte("short")}},
		{"empty id", "", map[string][]byte{"": key(1)}},
		{"no keys", "k1", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := NewKeyring(tt.current, tt.keys); err == nil {
				t.Fatal("want an error")
			}
		})
	}
}

func TestKeyringDoesNotShareMemoryWithItsInput(t *testing.T) {
	k := key(1)
	ring := newRing(t, "k1", map[string][]byte{"k1": k})
	sealed, id, _ := ring.Seal([]byte("x"), nil)

	k[0] ^= 0xff

	if _, err := ring.Open(sealed, id, nil); err != nil {
		t.Fatalf("changing the caller's slice broke the keyring: %v", err)
	}
}
