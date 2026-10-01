package hanko

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"time"
)

const (
	// TTL is how long a freshly issued token stays valid.
	TTL = 5 * time.Minute
	// maxKIDLen bounds the key id carried in the footer.
	maxKIDLen = 64
	jtiBytes  = 16
)

var (
	sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)
	kidFormat = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
)

// Claims is the approval payload bound to one exact draft version.
type Claims struct {
	ID         string    // jti, unique per token, used by the sender to reject replays
	Audience   string    // aud, the service allowed to consume the token
	Issuer     string    // iss, the service that signed it
	Subject    string    // sub, the approving owner
	DraftID    string    // draft_id
	Version    int64     // version of the draft that was approved
	BodySHA256 string    // body_sha256, lowercase hex of the approved body
	RcptSHA256 string    // rcpt_sha256, lowercase hex of the recipient set
	IssuedAt   time.Time // iat
	ExpiresAt  time.Time // exp
}

// NewClaims returns c with a fresh random ID, IssuedAt set to now and
// ExpiresAt set to now plus TTL. The input is not modified.
func NewClaims(now time.Time, c Claims) (Claims, error) {
	buf := make([]byte, jtiBytes)
	if _, err := rand.Read(buf); err != nil {
		return Claims{}, fmt.Errorf("hanko: generate jti: %w", err)
	}
	c.ID = hex.EncodeToString(buf)
	c.IssuedAt = now.UTC()
	c.ExpiresAt = now.UTC().Add(TTL)
	return c, nil
}

// validate checks the claims are complete and the lifetime is sane.
func (c Claims) validate() error {
	required := map[string]string{
		"jti": c.ID, "aud": c.Audience, "iss": c.Issuer, "sub": c.Subject, "draft_id": c.DraftID,
	}
	for name, v := range required {
		if v == "" {
			return fmt.Errorf("%w: %s is empty", ErrInvalidClaims, name)
		}
	}
	if c.Version < 1 {
		return fmt.Errorf("%w: version must be positive", ErrInvalidClaims)
	}
	if !sha256Hex.MatchString(c.BodySHA256) || !sha256Hex.MatchString(c.RcptSHA256) {
		return fmt.Errorf("%w: hashes must be lowercase hex sha256", ErrInvalidClaims)
	}
	if c.IssuedAt.IsZero() || !c.ExpiresAt.After(c.IssuedAt) {
		return fmt.Errorf("%w: exp must be after iat", ErrInvalidClaims)
	}
	if c.ExpiresAt.Sub(c.IssuedAt) > TTL {
		return fmt.Errorf("%w: lifetime exceeds %s", ErrInvalidClaims, TTL)
	}
	return nil
}

func validKID(kid string) bool {
	return len(kid) <= maxKIDLen && kidFormat.MatchString(kid)
}
