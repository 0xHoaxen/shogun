package httpauth

import (
	"net/http"
	"testing"
	"time"
)

func TestEveryCookieIsHTTPOnlySameSiteLaxAndSecureWhenAsked(t *testing.T) {
	flowKey := DeriveFlowKey([]byte("0123456789abcdef0123456789abcdef"))
	cookies := map[string]func(secure bool) *http.Cookie{
		"session":         func(secure bool) *http.Cookie { return SessionCookie("token", time.Now().Add(time.Hour), secure) },
		"cleared session": ClearedSessionCookie,
		"oauth flow":      func(secure bool) *http.Cookie { return flowCookie(flowKey, flow{State: "s", Verifier: "v"}, secure) },
		"cleared flow":    clearedFlowCookie,
	}
	wantPath := map[string]string{"session": "/", "cleared session": "/", "oauth flow": flowCookiePath, "cleared flow": flowCookiePath}

	for name, build := range cookies {
		for _, secure := range []bool{true, false} {
			t.Run(name, func(t *testing.T) {
				// Act
				c := build(secure)

				// Assert
				if !c.HttpOnly {
					t.Error("cookie is readable by scripts, want HttpOnly")
				}
				if c.SameSite != http.SameSiteLaxMode {
					t.Errorf("SameSite = %v, want Lax", c.SameSite)
				}
				if c.Secure != secure {
					t.Errorf("Secure = %v, want %v", c.Secure, secure)
				}
				if c.Path != wantPath[name] {
					t.Errorf("Path = %q, want %q", c.Path, wantPath[name])
				}
			})
		}
	}
}

func TestClearedCookiesExpireAtOnce(t *testing.T) {
	for name, c := range map[string]*http.Cookie{"session": ClearedSessionCookie(true), "flow": clearedFlowCookie(true)} {
		if c.MaxAge >= 0 || c.Value != "" {
			t.Errorf("%s: MaxAge=%d Value=%q, want a negative MaxAge and no value", name, c.MaxAge, c.Value)
		}
	}
}
