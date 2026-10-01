package hanko

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"
)

var epoch = time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func newKey(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func baseClaims(t *testing.T) Claims {
	t.Helper()
	c, err := NewClaims(epoch, Claims{
		Audience: "tsubame", Issuer: "fude", Subject: "owner",
		DraftID: "draft-1", Version: 3, BodySHA256: sum("body"), RcptSHA256: sum("a@b.c"),
	})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func baseExpected() Expected {
	return Expected{
		Audience: "tsubame", Issuer: "fude", DraftID: "draft-1", Version: 3,
		BodySHA256: sum("body"), RcptSHA256: sum("a@b.c"),
	}
}

func at(d time.Duration) Option { return WithClock(func() time.Time { return epoch.Add(d) }) }

func TestVerify(t *testing.T) {
	pubA, privA := newKey(t)
	pubB, privB := newKey(t)
	_, privEvil := newKey(t)

	sign := func(t *testing.T, mutate func(*Claims), key ed25519.PrivateKey, kid string) string {
		t.Helper()
		c := baseClaims(t)
		if mutate != nil {
			mutate(&c)
		}
		tok, err := Sign(c, key, kid)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	both := map[string]ed25519.PublicKey{"k1": pubA, "k2": pubB}

	tests := []struct {
		name    string
		token   func(t *testing.T) string
		keys    map[string]ed25519.PublicKey
		expect  func(Expected) Expected
		clock   time.Duration
		wantErr error
	}{
		{name: "valid", token: func(t *testing.T) string { return sign(t, nil, privA, "k1") }, keys: both},
		{
			name: "valid just before expiry", clock: TTL - time.Second,
			token: func(t *testing.T) string { return sign(t, nil, privA, "k1") }, keys: both,
		},
		{
			name: "expired at exp", clock: TTL, wantErr: ErrExpired,
			token: func(t *testing.T) string { return sign(t, nil, privA, "k1") }, keys: both,
		},
		{
			name: "issued in the future", clock: -time.Hour, wantErr: ErrNotYetValid,
			token: func(t *testing.T) string { return sign(t, nil, privA, "k1") }, keys: both,
		},
		{
			name: "wrong audience", wantErr: ErrAudience,
			token:  func(t *testing.T) string { return sign(t, nil, privA, "k1") },
			keys:   both,
			expect: func(e Expected) Expected { e.Audience = "other"; return e },
		},
		{
			name: "wrong issuer", wantErr: ErrIssuer,
			token:  func(t *testing.T) string { return sign(t, nil, privA, "k1") },
			keys:   both,
			expect: func(e Expected) Expected { e.Issuer = "other"; return e },
		},
		{
			name: "tampered body hash", wantErr: ErrHashMismatch,
			token: func(t *testing.T) string { return sign(t, func(c *Claims) { c.BodySHA256 = sum("evil") }, privA, "k1") },
			keys:  both,
		},
		{
			name: "tampered recipient hash", wantErr: ErrHashMismatch,
			token: func(t *testing.T) string {
				return sign(t, func(c *Claims) { c.RcptSHA256 = sum("x@y.z") }, privA, "k1")
			},
			keys: both,
		},
		{
			name: "stale version", wantErr: ErrDraftMismatch,
			token:  func(t *testing.T) string { return sign(t, nil, privA, "k1") },
			keys:   both,
			expect: func(e Expected) Expected { e.Version = 4; return e },
		},
		{
			name: "other draft", wantErr: ErrDraftMismatch,
			token:  func(t *testing.T) string { return sign(t, nil, privA, "k1") },
			keys:   both,
			expect: func(e Expected) Expected { e.DraftID = "draft-2"; return e },
		},
		{
			name: "wrong key under known kid", wantErr: ErrSignature,
			token: func(t *testing.T) string { return sign(t, nil, privEvil, "k1") }, keys: both,
		},
		{
			name: "kid swapped to another valid key", wantErr: ErrSignature,
			token: func(t *testing.T) string { return sign(t, nil, privA, "k2") }, keys: both,
		},
		{
			name:  "old and new key both verify (new)",
			token: func(t *testing.T) string { return sign(t, nil, privB, "k2") }, keys: both,
		},
		{
			name: "rotated out key is rejected", wantErr: ErrUnknownKey,
			token: func(t *testing.T) string { return sign(t, nil, privA, "k1") },
			keys:  map[string]ed25519.PublicKey{"k2": pubB},
		},
		{
			name: "empty key set", wantErr: ErrUnknownKey,
			token: func(t *testing.T) string { return sign(t, nil, privA, "k1") }, keys: nil,
		},
		{
			name: "garbage", wantErr: ErrMalformed,
			token: func(*testing.T) string { return "not-a-token" }, keys: both,
		},
		{
			name: "wrong protocol version", wantErr: ErrMalformed,
			token: func(*testing.T) string { return "v2.public.abcd" }, keys: both,
		},
		{
			name: "missing footer", wantErr: ErrMalformed,
			token: func(t *testing.T) string {
				tok := sign(t, nil, privA, "k1")
				return tok[:strings.LastIndex(tok, ".")]
			}, keys: both,
		},
		{
			name: "flipped payload byte", wantErr: ErrSignature,
			token: func(t *testing.T) string {
				tok := []byte(sign(t, nil, privA, "k1"))
				i := len(tokenPrefixV4) + 5
				if tok[i] == 'A' {
					tok[i] = 'B'
				} else {
					tok[i] = 'A'
				}
				return string(tok)
			}, keys: both,
		},
		{
			name: "incomplete expected", wantErr: ErrInvalidExpected,
			token:  func(t *testing.T) string { return sign(t, nil, privA, "k1") },
			keys:   both,
			expect: func(e Expected) Expected { e.Audience = ""; return e },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			exp := baseExpected()
			if tc.expect != nil {
				exp = tc.expect(exp)
			}

			got, err := Verify(tc.token(t), tc.keys, exp, at(tc.clock))

			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if got != (Claims{}) {
					t.Fatalf("claims must be zero on error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.DraftID != "draft-1" || got.Version != 3 || got.ID == "" || !got.ExpiresAt.Equal(epoch.Add(TTL)) {
				t.Fatalf("unexpected claims: %+v", got)
			}
		})
	}
}

func TestSignRejectsBadInput(t *testing.T) {
	_, priv := newKey(t)
	tests := []struct {
		name    string
		mutate  func(*Claims)
		key     ed25519.PrivateKey
		kid     string
		wantErr error
	}{
		{name: "empty audience", mutate: func(c *Claims) { c.Audience = "" }, key: priv, kid: "k1", wantErr: ErrInvalidClaims},
		{name: "zero version", mutate: func(c *Claims) { c.Version = 0 }, key: priv, kid: "k1", wantErr: ErrInvalidClaims},
		{name: "non-hex hash", mutate: func(c *Claims) { c.BodySHA256 = "XYZ" }, key: priv, kid: "k1", wantErr: ErrInvalidClaims},
		{name: "lifetime over TTL", mutate: func(c *Claims) { c.ExpiresAt = c.IssuedAt.Add(time.Hour) }, key: priv, kid: "k1", wantErr: ErrInvalidClaims},
		{name: "exp before iat", mutate: func(c *Claims) { c.ExpiresAt = c.IssuedAt.Add(-time.Second) }, key: priv, kid: "k1", wantErr: ErrInvalidClaims},
		{name: "empty kid", key: priv, kid: "", wantErr: ErrInvalidKey},
		{name: "kid with dot", key: priv, kid: "a.b/c", wantErr: ErrInvalidKey},
		{name: "short key", key: priv[:10], kid: "k1", wantErr: ErrInvalidKey},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := baseClaims(t)
			if tc.mutate != nil {
				tc.mutate(&c)
			}

			tok, err := Sign(c, tc.key, tc.kid)

			if !errors.Is(err, tc.wantErr) || tok != "" {
				t.Fatalf("got (%q, %v), want %v", tok, err, tc.wantErr)
			}
		})
	}
}

func TestNewClaimsIsUniqueAndDoesNotMutateInput(t *testing.T) {
	in := Claims{Audience: "a"}

	a, errA := NewClaims(epoch, in)
	b, errB := NewClaims(epoch, in)

	if errA != nil || errB != nil {
		t.Fatal(errA, errB)
	}
	if a.ID == b.ID || in.ID != "" || !a.ExpiresAt.Equal(epoch.Add(TTL)) {
		t.Fatalf("unexpected: %+v %+v %+v", a, b, in)
	}
}

func TestVerifyUsesWallClockByDefault(t *testing.T) {
	pub, priv := newKey(t)
	c, err := NewClaims(time.Now(), baseClaims(t))
	if err != nil {
		t.Fatal(err)
	}
	tok, err := Sign(c, priv, "k1")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := Verify(tok, map[string]ed25519.PublicKey{"k1": pub}, baseExpected()); err != nil {
		t.Fatalf("fresh token rejected: %v", err)
	}
}
