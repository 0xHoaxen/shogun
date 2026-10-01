// Package authz carries the caller's identity between services as a short-lived
// HMAC-signed token in gRPC metadata. torii mints it after a session check;
// every other service verifies it and fails closed.
package authz

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	// Header is the gRPC metadata key that carries the identity token.
	Header = "x-shogun-identity"
	// TokenTTL is how long a signed token stays valid.
	TokenTTL = 60 * time.Second
	// MinKeyLength is the shortest accepted signing key in bytes.
	MinKeyLength = 32
)

// Errors returned by Verify. Callers should treat all of them as unauthenticated.
var (
	ErrMalformed = errors.New("authz: malformed token")
	ErrSignature = errors.New("authz: bad signature")
	ErrExpired   = errors.New("authz: token expired")
	ErrNoOwner   = errors.New("authz: owner id is required")
	ErrShortKey  = errors.New("authz: signing key too short")
)

// Identity is who a call acts for.
type Identity struct {
	OwnerID   string
	RequestID string
}

type claims struct {
	OwnerID   string `json:"oid"`
	RequestID string `json:"rid,omitempty"`
	IssuedAt  int64  `json:"iat"`
	ExpiresAt int64  `json:"exp"`
}

// Authority signs and verifies identity tokens with one shared key.
type Authority struct {
	key []byte
	now func() time.Time
}

// Option customises an Authority.
type Option func(Authority) Authority

// WithClock replaces the time source, for deterministic tests.
func WithClock(now func() time.Time) Option {
	return func(a Authority) Authority {
		a.now = now
		return a
	}
}

// New returns an Authority for key. Keys shorter than MinKeyLength are rejected.
func New(key []byte, opts ...Option) (*Authority, error) {
	if len(key) < MinKeyLength {
		return nil, fmt.Errorf("%w: need at least %d bytes", ErrShortKey, MinKeyLength)
	}
	a := Authority{key: append([]byte(nil), key...), now: time.Now}
	for _, opt := range opts {
		a = opt(a)
	}
	return &a, nil
}

// Sign returns a token for id that expires TokenTTL from now.
func (a *Authority) Sign(id Identity) (string, error) {
	if id.OwnerID == "" {
		return "", ErrNoOwner
	}
	iat := a.now().UTC()
	payload, err := json.Marshal(claims{
		OwnerID:   id.OwnerID,
		RequestID: id.RequestID,
		IssuedAt:  iat.Unix(),
		ExpiresAt: iat.Add(TokenTTL).Unix(),
	})
	if err != nil {
		return "", fmt.Errorf("authz: encode claims: %w", err)
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	return body + "." + base64.RawURLEncoding.EncodeToString(a.mac(body)), nil
}

// Verify checks signature (constant time), expiry and owner, and returns the identity.
func (a *Authority) Verify(token string) (Identity, error) {
	body, sig, ok := strings.Cut(token, ".")
	if !ok || body == "" || sig == "" || strings.Contains(sig, ".") {
		return Identity{}, ErrMalformed
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return Identity{}, ErrMalformed
	}
	if !hmac.Equal(got, a.mac(body)) {
		return Identity{}, ErrSignature
	}
	raw, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return Identity{}, ErrMalformed
	}
	var c claims
	if err := json.Unmarshal(raw, &c); err != nil {
		return Identity{}, ErrMalformed
	}
	if c.OwnerID == "" {
		return Identity{}, ErrNoOwner
	}
	now := a.now().Unix()
	if c.ExpiresAt <= now || c.ExpiresAt-c.IssuedAt > int64(TokenTTL/time.Second) {
		return Identity{}, ErrExpired
	}
	return Identity{OwnerID: c.OwnerID, RequestID: c.RequestID}, nil
}

func (a *Authority) mac(body string) []byte {
	m := hmac.New(sha256.New, a.key)
	m.Write([]byte(body))
	return m.Sum(nil)
}

type ctxKey struct{}

// WithIdentity returns a context carrying id.
func WithIdentity(ctx context.Context, id Identity) context.Context {
	return context.WithValue(ctx, ctxKey{}, id)
}

// FromContext returns the identity in ctx, if any.
func FromContext(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}
