package authz

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

var testKey = []byte(strings.Repeat("k", MinKeyLength))

func fixedClock(t time.Time) Option { return WithClock(func() time.Time { return t }) }

func TestNewRejectsShortKey(t *testing.T) {
	if _, err := New([]byte("short")); !errors.Is(err, ErrShortKey) {
		t.Fatalf("err = %v, want ErrShortKey", err)
	}
}

func TestSignVerify(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	signer, _ := New(testKey, fixedClock(now))
	other, _ := New([]byte(strings.Repeat("x", MinKeyLength)), fixedClock(now))
	good, err := signer.Sign(Identity{OwnerID: "o1", RequestID: "r1"})
	if err != nil {
		t.Fatal(err)
	}
	body, sig, _ := strings.Cut(good, ".")
	flipped := body[:len(body)-1] + string(rune(body[len(body)-1]^1))

	tests := []struct {
		name     string
		verifier func() *Authority
		token    string
		want     error
	}{
		{"valid", func() *Authority { return signer }, good, nil},
		{"valid just before expiry", func() *Authority { a, _ := New(testKey, fixedClock(now.Add(59*time.Second))); return a }, good, nil},
		{"expired at ttl", func() *Authority { a, _ := New(testKey, fixedClock(now.Add(TokenTTL))); return a }, good, ErrExpired},
		{"wrong key", func() *Authority { return other }, good, ErrSignature},
		{"tampered body", func() *Authority { return signer }, flipped + "." + sig, ErrSignature},
		{"empty", func() *Authority { return signer }, "", ErrMalformed},
		{"no separator", func() *Authority { return signer }, "abc", ErrMalformed},
		{"extra segment", func() *Authority { return signer }, good + ".x", ErrMalformed},
		{"bad base64 sig", func() *Authority { return signer }, body + ".!!!", ErrMalformed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := tt.verifier().Verify(tt.token)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if err == nil && (id.OwnerID != "o1" || id.RequestID != "r1") {
				t.Fatalf("identity = %+v", id)
			}
			if err != nil && id != (Identity{}) {
				t.Fatalf("identity leaked on error: %+v", id)
			}
		})
	}
}

func TestSignRequiresOwner(t *testing.T) {
	a, _ := New(testKey)
	if _, err := a.Sign(Identity{}); !errors.Is(err, ErrNoOwner) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoadKey(t *testing.T) {
	look := func(v string, set bool) func(string) (string, bool) {
		return func(string) (string, bool) { return v, set }
	}
	if _, err := LoadKey(look("", false)); err == nil {
		t.Fatal("unset key accepted")
	}
	if _, err := LoadKey(look("short", true)); !errors.Is(err, ErrShortKey) {
		t.Fatalf("short key err = %v", err)
	}
	k, err := LoadKey(look(string(testKey), true))
	if err != nil || string(k) != string(testKey) {
		t.Fatalf("got %q, %v", k, err)
	}
}

func TestFromContext(t *testing.T) {
	if _, ok := FromContext(context.Background()); ok {
		t.Fatal("identity in empty ctx")
	}
	id, ok := FromContext(WithIdentity(context.Background(), Identity{OwnerID: "o"}))
	if !ok || id.OwnerID != "o" {
		t.Fatalf("got %+v, %v", id, ok)
	}
}
