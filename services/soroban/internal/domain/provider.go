package domain

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"

	"github.com/0xHoaxen/shogun/pkg/netguard"
)

// Errors of a provider a user entered. The messages never repeat the input, so a
// key cannot end up in a log or a response.
var (
	ErrInvalidProviderURL   = errors.New("provider address is not valid")
	ErrInvalidProviderModel = errors.New("provider model is not valid")
	ErrInvalidAPIKey        = errors.New("api key is not valid")
	ErrInvalidProviderPrice = errors.New("provider price is not valid")
	ErrInvalidTokenCap      = errors.New("token cap must be above zero")
)

const (
	maxBaseURLLength = 2048
	maxModelLength   = 200
	minAPIKeyLength  = 8
	maxAPIKeyLength  = 4096
	keyTail          = 4

	// maxMicrosPerMTok caps a price at a million dollars per million tokens, far
	// beyond any real model, so an overflow cannot be entered by mistake.
	maxMicrosPerMTok = 1_000_000_000_000
)

// internalSuffixes end the names that mean "inside the network". They are
// refused up front for a clear message; the dial-time check in netguard is the
// real defence, since a name can resolve anywhere.
var internalSuffixes = []string{".localhost", ".local", ".internal", ".svc", ".cluster.local", ".lan", ".home.arpa"}

// NormalizeBaseURL checks the Anthropic-compatible address a user entered and
// returns it in one form: https, lower-case host, no trailing slash. Plain http
// and addresses inside the network are refused unless allowInsecure, which only
// local development and tests turn on so a fake provider on localhost works.
func NormalizeBaseURL(raw string, allowInsecure bool) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > maxBaseURLLength {
		return "", ErrInvalidProviderURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return "", ErrInvalidProviderURL
	}
	if u.Scheme != "https" && (u.Scheme != "http" || !allowInsecure) {
		return "", ErrInvalidProviderURL
	}
	if u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery {
		return "", ErrInvalidProviderURL
	}
	host := strings.ToLower(u.Hostname())
	if !allowInsecure && !publicHost(host) {
		return "", ErrInvalidProviderURL
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawPath = strings.TrimRight(u.RawPath, "/")
	return u.String(), nil
}

// publicHost reports whether host could be a server on the public internet:
// a public IP address, or a dotted name that does not end like an internal one.
func publicHost(host string) bool {
	if addr, err := netip.ParseAddr(host); err == nil {
		return netguard.IsPublic(addr)
	}
	if !strings.Contains(host, ".") || host == "localhost" {
		return false
	}
	for _, suffix := range internalSuffixes {
		if strings.HasSuffix(host, suffix) {
			return false
		}
	}
	return true
}

// ValidateModel checks a model name. Names carry letters, digits and . _ : / @ + -.
func ValidateModel(model string) error {
	if model == "" || len(model) > maxModelLength {
		return ErrInvalidProviderModel
	}
	for _, r := range model {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case strings.ContainsRune("._:/@+-", r):
		default:
			return ErrInvalidProviderModel
		}
	}
	return nil
}

// ValidateAPIKey checks a key's shape without judging the provider: printable
// ASCII with no spaces, between 8 and 4096 characters.
func ValidateAPIKey(key string) error {
	if len(key) < minAPIKeyLength || len(key) > maxAPIKeyLength {
		return ErrInvalidAPIKey
	}
	for i := 0; i < len(key); i++ {
		if key[i] < 0x21 || key[i] > 0x7e {
			return ErrInvalidAPIKey
		}
	}
	return nil
}

// KeyLast4 is the tail of a key, kept in the clear to show which key is stored.
func KeyLast4(key string) string {
	if len(key) <= keyTail {
		return key
	}
	return key[len(key)-keyTail:]
}

// ValidateProviderPrices checks the optional price of a model soroban has no
// price for: both directions or neither, each between 0 and a sane maximum.
func ValidateProviderPrices(input, output *int64) error {
	if (input == nil) != (output == nil) {
		return fmt.Errorf("%w: set both prices or neither", ErrInvalidProviderPrice)
	}
	for _, p := range []*int64{input, output} {
		if p != nil && (*p < 0 || *p > maxMicrosPerMTok) {
			return fmt.Errorf("%w: out of range", ErrInvalidProviderPrice)
		}
	}
	return nil
}

// ValidateTokenCap checks the optional monthly token cap: unset, or above zero.
func ValidateTokenCap(capTokens *int64) error {
	if capTokens != nil && *capTokens <= 0 {
		return ErrInvalidTokenCap
	}
	return nil
}
