// Package settings reads the environment variables only torii uses: the public
// API listener, Google login, the owner allowlist and request limits. The
// variables every service shares live in pkg/config.
package settings

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/0xHoaxen/shogun/pkg/config"
)

const (
	defaultPublicAddr = ":8081"
	defaultPublicURL  = "http://localhost:3000"
	defaultIssuer     = "https://accounts.google.com"
	defaultSessionTTL = 7 * 24 * time.Hour
	defaultRateLimit  = 20
	defaultRateBurst  = 40
	defaultMaxBody    = 8 << 20
)

// Settings is torii's own configuration.
type Settings struct {
	// PublicAddr is where the browser-facing API listens.
	PublicAddr string
	// PublicURL is the origin the browser uses, without a trailing slash. The
	// web app proxies /api and /auth to torii, so it is the web app's origin.
	PublicURL          string
	GoogleClientID     string
	GoogleClientSecret string
	// GoogleIssuerURL is Google's issuer; tests point it at a fake provider.
	GoogleIssuerURL string
	AllowedEmails   []string
	// KagamiAddr is the gRPC address of kagami, which owns jobs and contacts.
	KagamiAddr string
	// SorobanAddr is the gRPC address of soroban, which meters Claude spend.
	SorobanAddr string
	SessionTTL  time.Duration
	// RateLimit is requests per second per client; RateBurst the burst size.
	RateLimit int
	RateBurst int
	// MaxBodyBytes caps a request body; the CSV import is the largest.
	MaxBodyBytes int
}

// RedirectURL is the OAuth redirect URI registered with Google.
func (s Settings) RedirectURL() string { return s.PublicURL + "/auth/callback" }

// SecureCookies reports whether cookies get the Secure flag: whenever the
// browser reaches torii over https.
func (s Settings) SecureCookies() bool { return strings.HasPrefix(s.PublicURL, "https://") }

// Load reads and validates the settings. Missing login settings are an error
// so torii never starts open.
func Load(lookup config.LookupFunc) (Settings, error) {
	var errs []error
	collect := func(err error) {
		if err != nil {
			errs = append(errs, err)
		}
	}
	clientID, err := config.Required(lookup, "GOOGLE_CLIENT_ID")
	collect(err)
	clientSecret, err := config.Required(lookup, "GOOGLE_CLIENT_SECRET")
	collect(err)
	emails, err := config.Required(lookup, "TORII_ALLOWED_EMAILS")
	collect(err)
	kagamiAddr, err := config.Required(lookup, "KAGAMI_ADDR")
	collect(err)
	sorobanAddr, err := config.Required(lookup, "SOROBAN_ADDR")
	collect(err)
	ttl, err := config.Duration(lookup, "SESSION_TTL", defaultSessionTTL)
	collect(err)
	rateLimit, err := config.Int(lookup, "TORII_RATE_LIMIT", defaultRateLimit)
	collect(err)
	rateBurst, err := config.Int(lookup, "TORII_RATE_BURST", defaultRateBurst)
	collect(err)
	maxBody, err := config.Int(lookup, "TORII_MAX_BODY_BYTES", defaultMaxBody)
	collect(err)

	s := Settings{
		PublicAddr:         config.String(lookup, "TORII_PUBLIC_ADDR", defaultPublicAddr),
		PublicURL:          strings.TrimRight(config.String(lookup, "TORII_PUBLIC_URL", defaultPublicURL), "/"),
		GoogleClientID:     clientID,
		GoogleClientSecret: clientSecret,
		GoogleIssuerURL:    config.String(lookup, "GOOGLE_ISSUER_URL", defaultIssuer),
		AllowedEmails:      splitList(emails),
		KagamiAddr:         kagamiAddr,
		SorobanAddr:        sorobanAddr,
		SessionTTL:         ttl,
		RateLimit:          rateLimit,
		RateBurst:          rateBurst,
		MaxBodyBytes:       maxBody,
	}
	collect(s.validate())
	if err := errors.Join(errs...); err != nil {
		return Settings{}, err
	}
	return s, nil
}

func (s Settings) validate() error {
	var errs []error
	if u, err := url.Parse(s.PublicURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("settings: TORII_PUBLIC_URL %q must be an http(s) URL", s.PublicURL))
	}
	if len(s.AllowedEmails) == 0 && s.GoogleClientID != "" {
		errs = append(errs, errors.New("settings: TORII_ALLOWED_EMAILS has no addresses"))
	}
	if s.SessionTTL <= 0 {
		errs = append(errs, errors.New("settings: SESSION_TTL must be positive"))
	}
	if s.RateLimit <= 0 || s.RateBurst <= 0 {
		errs = append(errs, errors.New("settings: TORII_RATE_LIMIT and TORII_RATE_BURST must be positive"))
	}
	if s.MaxBodyBytes <= 0 {
		errs = append(errs, errors.New("settings: TORII_MAX_BODY_BYTES must be positive"))
	}
	return errors.Join(errs...)
}

func splitList(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
