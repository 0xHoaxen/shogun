// Package idptest is a fake OpenID Connect identity provider for tests. It
// serves discovery, keys and a token endpoint that enforces PKCE, and signs
// real RS256 ID tokens, so the production OIDC code runs unchanged against it.
package idptest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

const (
	keyID         = "idptest-key"
	rsaBits       = 2048
	tokenLifetime = time.Hour
	codeBytes     = 16
)

// Claims is the identity the fake provider vouches for in an ID token.
type Claims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
}

type issuedCode struct {
	challenge string
	claims    Claims
}

// IDP is a running fake identity provider.
type IDP struct {
	server       *httptest.Server
	key          *rsa.PrivateKey
	clientID     string
	clientSecret string

	mu    sync.Mutex
	codes map[string]issuedCode
}

// New starts a fake provider that accepts the given client, and stops it when
// the test ends.
func New(t testing.TB, clientID, clientSecret string) *IDP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, rsaBits)
	if err != nil {
		t.Fatalf("idptest: generate key: %v", err)
	}
	idp := &IDP{key: key, clientID: clientID, clientSecret: clientSecret, codes: map[string]issuedCode{}}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /.well-known/openid-configuration", idp.discovery)
	mux.HandleFunc("GET /keys", idp.keys)
	mux.HandleFunc("POST /token", idp.token)
	idp.server = httptest.NewServer(mux)
	t.Cleanup(idp.server.Close)
	return idp
}

// IssuerURL is the issuer to give to the OIDC client.
func (i *IDP) IssuerURL() string { return i.server.URL }

// IssueCode registers an authorization code for the PKCE challenge the browser
// sent, as a real provider does once the user has signed in. The code is
// single use.
func (i *IDP) IssueCode(codeChallenge string, claims Claims) string {
	buf := make([]byte, codeBytes)
	_, _ = rand.Read(buf)
	code := base64.RawURLEncoding.EncodeToString(buf)
	i.mu.Lock()
	defer i.mu.Unlock()
	i.codes[code] = issuedCode{challenge: codeChallenge, claims: claims}
	return code
}

func (i *IDP) discovery(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{
		"issuer":                                i.server.URL,
		"authorization_endpoint":                i.server.URL + "/authorize",
		"token_endpoint":                        i.server.URL + "/token",
		"jwks_uri":                              i.server.URL + "/keys",
		"response_types_supported":              []string{"code"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
	})
}

func (i *IDP) keys(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{
		Key: &i.key.PublicKey, KeyID: keyID, Algorithm: string(jose.RS256), Use: "sig",
	}}})
}

func (i *IDP) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || r.PostForm.Get("grant_type") != "authorization_code" {
		http.Error(w, `{"error":"invalid_request"}`, http.StatusBadRequest)
		return
	}
	if !i.authenticClient(r) {
		http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
		return
	}
	issued, ok := i.redeem(r.PostForm.Get("code"))
	if !ok || pkceChallenge(r.PostForm.Get("code_verifier")) != issued.challenge {
		http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
		return
	}
	idToken, err := i.signIDToken(issued.claims)
	if err != nil {
		http.Error(w, `{"error":"server_error"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"access_token": "idptest-access-token",
		"token_type":   "Bearer",
		"expires_in":   int(tokenLifetime.Seconds()),
		"id_token":     idToken,
	})
}

func (i *IDP) authenticClient(r *http.Request) bool {
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	return id == i.clientID && secret == i.clientSecret
}

func (i *IDP) redeem(code string) (issuedCode, bool) {
	i.mu.Lock()
	defer i.mu.Unlock()
	issued, ok := i.codes[code]
	delete(i.codes, code)
	return issued, ok
}

func (i *IDP) signIDToken(c Claims) (string, error) {
	signer, err := jose.NewSigner(
		jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: i.key, KeyID: keyID}},
		(&jose.SignerOptions{}).WithType("JWT"),
	)
	if err != nil {
		return "", err
	}
	now := time.Now()
	standard := jwt.Claims{
		Issuer:   i.server.URL,
		Subject:  c.Subject,
		Audience: jwt.Audience{i.clientID},
		IssuedAt: jwt.NewNumericDate(now),
		Expiry:   jwt.NewNumericDate(now.Add(tokenLifetime)),
	}
	extra := map[string]any{"email": c.Email, "email_verified": c.EmailVerified, "name": c.Name}
	return jwt.Signed(signer).Claims(standard).Claims(extra).Serialize()
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}
