// Package hanko signs and verifies the approval token that gates every
// external send. The token is a PASETO v4.public message whose footer is the
// key id, so two public keys can be trusted at once during rotation.
//
// Only fude signs; only tsubame verifies. Verify fails closed: every pin in
// Expected is mandatory and any doubt yields an error.
package hanko

import (
	"crypto/ed25519"
	"crypto/subtle"
	"errors"
	"fmt"
	"strings"
	"time"

	paseto "aidanwoods.dev/go-paseto"
)

// iatLeeway tolerates small clock differences between signer and verifier.
const iatLeeway = 30 * time.Second

const (
	claimJTI      = "jti"
	claimAud      = "aud"
	claimIss      = "iss"
	claimSub      = "sub"
	claimDraftID  = "draft_id"
	claimVersion  = "version"
	claimBodySHA  = "body_sha256"
	claimRcptSHA  = "rcpt_sha256"
	claimIAT      = "iat"
	claimEXP      = "exp"
	tokenPrefixV4 = "v4.public."
)

// Expected pins what the verifier requires. Every field is mandatory.
type Expected struct {
	Audience   string
	Issuer     string
	DraftID    string
	Version    int64
	BodySHA256 string
	RcptSHA256 string
}

func (e Expected) validate() error {
	if e.Audience == "" || e.Issuer == "" || e.DraftID == "" || e.Version < 1 ||
		!sha256Hex.MatchString(e.BodySHA256) || !sha256Hex.MatchString(e.RcptSHA256) {
		return ErrInvalidExpected
	}
	return nil
}

// Option customises Verify.
type Option func(*verifyConfig)

type verifyConfig struct{ now func() time.Time }

// WithClock replaces the time source, for tests.
func WithClock(now func() time.Time) Option {
	return func(c *verifyConfig) { c.now = now }
}

// Sign returns a v4.public token for claims, signed with key and labelled kid.
func Sign(claims Claims, key ed25519.PrivateKey, kid string) (string, error) {
	if err := claims.validate(); err != nil {
		return "", err
	}
	if !validKID(kid) {
		return "", fmt.Errorf("%w: kid must be 1-%d chars of [A-Za-z0-9._-]", ErrInvalidKey, maxKIDLen)
	}
	if len(key) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("%w: bad private key length", ErrInvalidKey)
	}
	secret, err := paseto.NewV4AsymmetricSecretKeyFromEd25519(key)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}

	tok := paseto.NewToken()
	tok.SetString(claimJTI, claims.ID)
	tok.SetString(claimAud, claims.Audience)
	tok.SetString(claimIss, claims.Issuer)
	tok.SetString(claimSub, claims.Subject)
	tok.SetString(claimDraftID, claims.DraftID)
	tok.SetString(claimBodySHA, claims.BodySHA256)
	tok.SetString(claimRcptSHA, claims.RcptSHA256)
	if err := tok.Set(claimVersion, claims.Version); err != nil {
		return "", fmt.Errorf("hanko: set version: %w", err)
	}
	tok.SetTime(claimIAT, claims.IssuedAt.UTC())
	tok.SetTime(claimEXP, claims.ExpiresAt.UTC())
	tok.SetFooter([]byte(kid))
	return tok.V4Sign(secret, nil), nil
}

// Verify checks token against the trusted keys (by kid) and expected pins and
// returns its claims. Any failure returns zero Claims and a wrapped sentinel.
func Verify(token string, keys map[string]ed25519.PublicKey, expected Expected, opts ...Option) (Claims, error) {
	cfg := verifyConfig{now: time.Now}
	for _, o := range opts {
		o(&cfg)
	}
	if err := expected.validate(); err != nil {
		return Claims{}, err
	}
	kid, err := footerKID(token)
	if err != nil {
		return Claims{}, err
	}
	pub, ok := keys[kid]
	if !ok {
		return Claims{}, fmt.Errorf("%w: %q", ErrUnknownKey, kid)
	}
	public, err := paseto.NewV4AsymmetricPublicKeyFromEd25519(pub)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %w", ErrInvalidKey, err)
	}
	// Expiry and all other rules are checked below against the injected clock.
	parser := paseto.NewParserWithoutExpiryCheck()
	tok, err := parser.ParseV4Public(public, token, nil)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %w", ErrSignature, err)
	}
	claims, err := readClaims(tok)
	if err != nil {
		return Claims{}, err
	}
	if err := check(claims, expected, cfg.now().UTC()); err != nil {
		return Claims{}, err
	}
	return claims, nil
}

// footerKID extracts the unauthenticated kid; the signature check that follows
// covers the footer, so a swapped kid fails verification.
func footerKID(token string) (string, error) {
	if !strings.HasPrefix(token, tokenPrefixV4) {
		return "", fmt.Errorf("%w: not a v4.public token", ErrMalformed)
	}
	footer, err := paseto.NewParserWithoutExpiryCheck().UnsafeParseFooter(paseto.V4Public, token)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrMalformed, err)
	}
	kid := string(footer)
	if !validKID(kid) {
		return "", fmt.Errorf("%w: bad kid in footer", ErrMalformed)
	}
	return kid, nil
}

func readClaims(tok *paseto.Token) (Claims, error) {
	var c Claims
	var errs []error
	get := func(name string, dst *string) {
		v, err := tok.GetString(name)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			return
		}
		*dst = v
	}
	get(claimJTI, &c.ID)
	get(claimAud, &c.Audience)
	get(claimIss, &c.Issuer)
	get(claimSub, &c.Subject)
	get(claimDraftID, &c.DraftID)
	get(claimBodySHA, &c.BodySHA256)
	get(claimRcptSHA, &c.RcptSHA256)
	if err := tok.Get(claimVersion, &c.Version); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", claimVersion, err))
	}
	var err error
	if c.IssuedAt, err = tok.GetTime(claimIAT); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", claimIAT, err))
	}
	if c.ExpiresAt, err = tok.GetTime(claimEXP); err != nil {
		errs = append(errs, fmt.Errorf("%s: %w", claimEXP, err))
	}
	if len(errs) > 0 {
		return Claims{}, fmt.Errorf("%w: %w", ErrInvalidClaims, errors.Join(errs...))
	}
	if err := c.validate(); err != nil {
		return Claims{}, err
	}
	return c, nil
}

func check(c Claims, e Expected, now time.Time) error {
	if !now.Before(c.ExpiresAt) {
		return ErrExpired
	}
	if c.IssuedAt.After(now.Add(iatLeeway)) {
		return ErrNotYetValid
	}
	if !equal(c.Audience, e.Audience) {
		return ErrAudience
	}
	if !equal(c.Issuer, e.Issuer) {
		return ErrIssuer
	}
	if !equal(c.DraftID, e.DraftID) || c.Version != e.Version {
		return ErrDraftMismatch
	}
	bodyOK := equal(c.BodySHA256, e.BodySHA256)
	rcptOK := equal(c.RcptSHA256, e.RcptSHA256)
	if !bodyOK || !rcptOK {
		return ErrHashMismatch
	}
	return nil
}

func equal(a, b string) bool {
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
