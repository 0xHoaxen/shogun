// Package settings reads the environment variables only torii uses: the public
// API listener, Google login, the admin list and request limits. The
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
	// The per-address limit is a flood guard. Behind the web app's proxy every
	// browser shares one address, so it must sit far above what a few dozen users
	// make together; each signed-in user is limited separately by RateLimit.
	defaultIPRateLimit = 500
	defaultIPRateBurst = 1000
	defaultMaxBody     = 8 << 20
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
	// AdminEmails become admins when they sign in. Anyone else with a verified
	// Google account may sign up as a member.
	AdminEmails []string
	// KagamiAddr is the gRPC address of kagami, which owns jobs and contacts.
	KagamiAddr string
	// FudeAddr is the gRPC address of fude, which owns drafts and approvals.
	FudeAddr string
	// TsubameAddr is the gRPC address of tsubame, which owns mail accounts.
	TsubameAddr string
	// TaikoAddr is the gRPC address of taiko, which owns notifications.
	TaikoAddr string
	// DojoAddr is the gRPC address of dojo, which owns the learning log.
	DojoAddr string
	// KatanaAddr is the gRPC address of katana, which owns profile suggestions.
	KatanaAddr string
	// ShinobiAddr is the gRPC address of shinobi, which owns job discovery.
	ShinobiAddr string
	// SenseiAddr is the gRPC address of sensei, which owns analytics.
	SenseiAddr string
	// SorobanAddr is the gRPC address of soroban, which meters Claude spend.
	SorobanAddr string
	SessionTTL  time.Duration
	// RateLimit is requests per second per signed-in user; RateBurst the burst size.
	RateLimit int
	RateBurst int
	// IPRateLimit is requests per second per remote address, and IPRateBurst the
	// burst size. It is a flood guard shared by every browser behind the proxy.
	IPRateLimit int
	IPRateBurst int
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
	emails, err := config.Required(lookup, "TORII_ADMIN_EMAILS")
	collect(err)
	kagamiAddr, err := config.Required(lookup, "KAGAMI_ADDR")
	collect(err)
	sorobanAddr, err := config.Required(lookup, "SOROBAN_ADDR")
	collect(err)
	fudeAddr, err := config.Required(lookup, "FUDE_ADDR")
	collect(err)
	tsubameAddr, err := config.Required(lookup, "TSUBAME_ADDR")
	collect(err)
	taikoAddr, err := config.Required(lookup, "TAIKO_ADDR")
	collect(err)
	dojoAddr, err := config.Required(lookup, "DOJO_ADDR")
	collect(err)
	katanaAddr, err := config.Required(lookup, "KATANA_ADDR")
	collect(err)
	shinobiAddr, err := config.Required(lookup, "SHINOBI_ADDR")
	collect(err)
	senseiAddr, err := config.Required(lookup, "SENSEI_ADDR")
	collect(err)
	ttl, err := config.Duration(lookup, "SESSION_TTL", defaultSessionTTL)
	collect(err)
	rateLimit, err := config.Int(lookup, "TORII_RATE_LIMIT", defaultRateLimit)
	collect(err)
	rateBurst, err := config.Int(lookup, "TORII_RATE_BURST", defaultRateBurst)
	collect(err)
	ipRateLimit, err := config.Int(lookup, "TORII_IP_RATE_LIMIT", defaultIPRateLimit)
	collect(err)
	ipRateBurst, err := config.Int(lookup, "TORII_IP_RATE_BURST", defaultIPRateBurst)
	collect(err)
	maxBody, err := config.Int(lookup, "TORII_MAX_BODY_BYTES", defaultMaxBody)
	collect(err)

	s := Settings{
		PublicAddr:         config.String(lookup, "TORII_PUBLIC_ADDR", defaultPublicAddr),
		PublicURL:          strings.TrimRight(config.String(lookup, "TORII_PUBLIC_URL", defaultPublicURL), "/"),
		GoogleClientID:     clientID,
		GoogleClientSecret: clientSecret,
		GoogleIssuerURL:    config.String(lookup, "GOOGLE_ISSUER_URL", defaultIssuer),
		AdminEmails:        splitList(emails),
		KagamiAddr:         kagamiAddr,
		FudeAddr:           fudeAddr,
		TsubameAddr:        tsubameAddr,
		TaikoAddr:          taikoAddr,
		DojoAddr:           dojoAddr,
		KatanaAddr:         katanaAddr,
		ShinobiAddr:        shinobiAddr,
		SenseiAddr:         senseiAddr,
		SorobanAddr:        sorobanAddr,
		SessionTTL:         ttl,
		RateLimit:          rateLimit,
		RateBurst:          rateBurst,
		IPRateLimit:        ipRateLimit,
		IPRateBurst:        ipRateBurst,
		MaxBodyBytes:       maxBody,
	}
	collect(s.validate())
	if config.String(lookup, "ENVIRONMENT", config.EnvLocal) == config.EnvProduction && !s.SecureCookies() {
		// Without https the session cookie would go out without the Secure flag.
		collect(fmt.Errorf("settings: TORII_PUBLIC_URL %q must be https in production", s.PublicURL))
	}
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
	if len(s.AdminEmails) == 0 && s.GoogleClientID != "" {
		errs = append(errs, errors.New("settings: TORII_ADMIN_EMAILS has no addresses"))
	}
	if s.SessionTTL <= 0 {
		errs = append(errs, errors.New("settings: SESSION_TTL must be positive"))
	}
	if s.IPRateLimit <= 0 || s.IPRateBurst <= 0 {
		errs = append(errs, errors.New("settings: TORII_IP_RATE_LIMIT and TORII_IP_RATE_BURST must be positive"))
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
