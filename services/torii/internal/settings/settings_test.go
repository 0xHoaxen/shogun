package settings_test

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/services/torii/internal/settings"
)

func lookupOf(env map[string]string) func(string) (string, bool) {
	return func(key string) (string, bool) {
		v, ok := env[key]
		return v, ok
	}
}

func validEnv() map[string]string {
	return map[string]string{
		"GOOGLE_CLIENT_ID":     "client-id",
		"GOOGLE_CLIENT_SECRET": "client-secret",
		"TORII_ALLOWED_EMAILS": "owner@example.com",
		"KAGAMI_ADDR":          "kagami:9090",
		"SOROBAN_ADDR":         "soroban:9090",
		"FUDE_ADDR":            "fude:9090",
		"TSUBAME_ADDR":         "tsubame:9090",
	}
}

func TestLoadAppliesDefaults(t *testing.T) {
	// Act
	got, err := settings.Load(lookupOf(validEnv()))
	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.PublicAddr != ":8081" || got.PublicURL != "http://localhost:3000" ||
		got.GoogleIssuerURL != "https://accounts.google.com" || got.SessionTTL != 7*24*time.Hour ||
		got.RateLimit != 20 || got.RateBurst != 40 || got.MaxBodyBytes != 8<<20 {
		t.Fatalf("defaults wrong: %+v", got)
	}
	if got.RedirectURL() != "http://localhost:3000/auth/callback" || got.SecureCookies() {
		t.Fatalf("redirect %q secure %v", got.RedirectURL(), got.SecureCookies())
	}
}

func TestLoadParsesOverrides(t *testing.T) {
	// Arrange
	env := validEnv()
	env["TORII_ALLOWED_EMAILS"] = " a@example.com, b@example.com ,,"
	env["TORII_PUBLIC_URL"] = "https://shogun.example.com/"
	env["SESSION_TTL"] = "48h"

	// Act
	got, err := settings.Load(lookupOf(env))
	// Assert
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if want := []string{"a@example.com", "b@example.com"}; !reflect.DeepEqual(got.AllowedEmails, want) {
		t.Fatalf("AllowedEmails = %v, want %v", got.AllowedEmails, want)
	}
	if got.PublicURL != "https://shogun.example.com" || !got.SecureCookies() || got.SessionTTL != 48*time.Hour {
		t.Fatalf("overrides wrong: %+v", got)
	}
}

func TestLoadRejectsBadConfig(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(env map[string]string)
		wantMsg string
	}{
		{"missing client id", func(e map[string]string) { delete(e, "GOOGLE_CLIENT_ID") }, "GOOGLE_CLIENT_ID"},
		{"missing client secret", func(e map[string]string) { delete(e, "GOOGLE_CLIENT_SECRET") }, "GOOGLE_CLIENT_SECRET"},
		{"missing allowlist", func(e map[string]string) { delete(e, "TORII_ALLOWED_EMAILS") }, "TORII_ALLOWED_EMAILS"},
		{"missing kagami address", func(e map[string]string) { delete(e, "KAGAMI_ADDR") }, "KAGAMI_ADDR"},
		{"missing soroban address", func(e map[string]string) { delete(e, "SOROBAN_ADDR") }, "SOROBAN_ADDR"},
		{"missing fude address", func(e map[string]string) { delete(e, "FUDE_ADDR") }, "FUDE_ADDR"},
		{"missing tsubame address", func(e map[string]string) { delete(e, "TSUBAME_ADDR") }, "TSUBAME_ADDR"},
		{"blank allowlist", func(e map[string]string) { e["TORII_ALLOWED_EMAILS"] = " , " }, "no addresses"},
		{"public url not http", func(e map[string]string) { e["TORII_PUBLIC_URL"] = "ftp://x" }, "TORII_PUBLIC_URL"},
		{"zero ttl", func(e map[string]string) { e["SESSION_TTL"] = "0s" }, "SESSION_TTL"},
		{"zero rate limit", func(e map[string]string) { e["TORII_RATE_LIMIT"] = "0" }, "TORII_RATE_LIMIT"},
		{"zero body cap", func(e map[string]string) { e["TORII_MAX_BODY_BYTES"] = "0" }, "TORII_MAX_BODY_BYTES"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			env := validEnv()
			tt.mutate(env)

			// Act
			_, err := settings.Load(lookupOf(env))

			// Assert
			if err == nil || !strings.Contains(err.Error(), tt.wantMsg) {
				t.Fatalf("err = %v, want mention of %q", err, tt.wantMsg)
			}
		})
	}
}
