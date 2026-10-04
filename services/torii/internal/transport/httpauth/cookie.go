package httpauth

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

const (
	// SessionCookieName is the cookie that carries the session token.
	SessionCookieName = "shogun_session"

	flowCookieName = "shogun_oauth"
	flowCookiePath = "/auth"
	flowTTL        = 10 * time.Minute
	flowKeyLabel   = "torii-oauth-flow"
	minFlowKeySize = 32
)

var errInvalidFlowCookie = errors.New("invalid oauth flow cookie")

// SessionCookie returns the cookie that signs the browser in until expires.
func SessionCookie(token string, expires time.Time, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     SessionCookieName,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// ClearedSessionCookie returns the cookie that signs the browser out.
func ClearedSessionCookie(secure bool) *http.Cookie {
	cookie := SessionCookie("", time.Unix(0, 0), secure)
	cookie.MaxAge = -1
	return cookie
}

// DeriveFlowKey derives the key that seals the OAuth flow cookie from the
// service's identity signing key, so no extra secret has to be configured.
func DeriveFlowKey(identityKey []byte) []byte {
	mac := hmac.New(sha256.New, identityKey)
	mac.Write([]byte(flowKeyLabel))
	return mac.Sum(nil)
}

// flow is the state kept in the browser between /auth/login and /auth/callback.
type flow struct {
	State     string `json:"s"`
	Verifier  string `json:"v"`
	ExpiresAt int64  `json:"e"`
}

func flowCookie(key []byte, f flow, secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     flowCookieName,
		Value:    sealFlow(key, f),
		Path:     flowCookiePath,
		MaxAge:   int(flowTTL.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

func clearedFlowCookie(secure bool) *http.Cookie {
	return &http.Cookie{
		Name:     flowCookieName,
		Path:     flowCookiePath,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// sealFlow encodes f as base64(json) + "." + base64(hmac), so the browser can
// hold it but not alter it.
func sealFlow(key []byte, f flow) string {
	body, _ := json.Marshal(f) // a struct of strings and an int cannot fail to marshal
	encoded := base64.RawURLEncoding.EncodeToString(body)
	return encoded + "." + base64.RawURLEncoding.EncodeToString(sign(key, encoded))
}

// openFlow verifies and decodes a sealed flow cookie value.
func openFlow(key []byte, value string, now time.Time) (flow, error) {
	encoded, signature, found := strings.Cut(value, ".")
	if !found {
		return flow{}, errInvalidFlowCookie
	}
	got, err := base64.RawURLEncoding.DecodeString(signature)
	if err != nil || subtle.ConstantTimeCompare(got, sign(key, encoded)) != 1 {
		return flow{}, errInvalidFlowCookie
	}
	body, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return flow{}, errInvalidFlowCookie
	}
	var f flow
	if err := json.Unmarshal(body, &f); err != nil {
		return flow{}, errInvalidFlowCookie
	}
	if !now.Before(time.Unix(f.ExpiresAt, 0)) {
		return flow{}, errInvalidFlowCookie
	}
	return f, nil
}

func sign(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}
