package gmail

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	netmail "net/mail"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail"
)

const (
	refreshToken = "1//refresh-secret-value"
	accessToken  = "ya29.access-secret-value"
)

// google is a fake of both the token endpoint and the Gmail API.
type google struct {
	srv *httptest.Server

	mu          sync.Mutex
	tokenCalls  []url.Values
	revoked     bool
	apiStatus   int // when set, API calls answer this status with an error body
	sent        map[string]any
	lastQuery   url.Values
	lastAuth    string
	historyBody string
}

func newGoogle(t *testing.T) *google {
	t.Helper()
	g := &google{}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", g.token)
	mux.HandleFunc("/gmail/v1/users/me/", g.api)
	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)
	return g
}

func (g *google) token(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	g.mu.Lock()
	g.tokenCalls = append(g.tokenCalls, r.PostForm)
	revoked := g.revoked
	g.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if revoked || r.PostForm.Get("refresh_token") != refreshToken {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, `{"error":"invalid_grant","error_description":"Token has been expired or revoked."}`)
		return
	}
	_, _ = io.WriteString(w, `{"access_token":"`+accessToken+`","token_type":"Bearer","expires_in":3600}`)
}

func (g *google) api(w http.ResponseWriter, r *http.Request) {
	g.mu.Lock()
	g.lastQuery, g.lastAuth = r.URL.Query(), r.Header.Get("Authorization")
	status, history := g.apiStatus, g.historyBody
	g.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	if g.lastAuth != "Bearer "+accessToken {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"error":{"message":"bad credentials"}}`)
		return
	}
	if status != 0 {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error":{"status":"RESOURCE_EXHAUSTED","message":"quota exceeded for user `+refreshToken+`"}}`)
		return
	}
	switch path := strings.TrimPrefix(r.URL.Path, "/gmail/v1/users/me/"); {
	case path == "profile":
		_, _ = io.WriteString(w, `{"emailAddress":"Me@Example.com","historyId":"900"}`)
	case path == "messages" && r.Method == http.MethodGet:
		if r.URL.Query().Get("pageToken") == "" {
			_, _ = io.WriteString(w, `{"messages":[{"id":"m1","threadId":"t1"},{"id":"m2","threadId":"t2"}],"nextPageToken":"p2"}`)
			return
		}
		_, _ = io.WriteString(w, `{"messages":[{"id":"m3","threadId":"t3"}]}`)
	case path == "messages/send":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		g.mu.Lock()
		g.sent = body
		g.mu.Unlock()
		_, _ = io.WriteString(w, `{"id":"sent-1","threadId":"t-sent"}`)
	case path == "messages/m1":
		_, _ = io.WriteString(w, `{"id":"m1","threadId":"t1","labelIds":["INBOX"],"snippet":"We received your application","internalDate":"1790000000000",
			"payload":{"headers":[{"name":"From","value":"Recruiter <HR@Lumen.example>"},{"name":"To","value":"me@example.com, Other <o@x.example>"},{"name":"Subject","value":"Thanks for applying"}]}}`)
	case path == "messages/m-sent":
		_, _ = io.WriteString(w, `{"id":"m-sent","threadId":"t9","labelIds":["SENT"],"snippet":"hi","internalDate":"1790000100000",
			"payload":{"headers":[{"name":"From","value":"me@example.com"},{"name":"To","value":"a@b.example"},{"name":"X-Shogun-Draft","value":"draft-7"}]}}`)
	case path == "history":
		if history != "" {
			_, _ = io.WriteString(w, history)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"message":"Requested entity was not found."}}`)
	default:
		w.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(w, `{"error":{"message":"Requested entity was not found."}}`)
	}
}

func newProvider(t *testing.T, g *google, token string) mail.Provider {
	t.Helper()
	c, err := New(Config{ClientID: "id", ClientSecret: "secret", APIBase: g.srv.URL, TokenURL: g.srv.URL + "/token", AuthURL: g.srv.URL + "/auth"})
	if err != nil {
		t.Fatal(err)
	}
	return c.Provider(token)
}

func TestProfileExchangesTheRefreshTokenForAnAccessToken(t *testing.T) {
	g := newGoogle(t)

	p, err := newProvider(t, g, refreshToken).Profile(context.Background())

	if err != nil || p.Address != "me@example.com" || p.HistoryID != "900" {
		t.Fatalf("got %+v, %v", p, err)
	}
	if len(g.tokenCalls) != 1 || g.tokenCalls[0].Get("grant_type") != "refresh_token" || g.tokenCalls[0].Get("refresh_token") != refreshToken {
		t.Fatalf("token calls %+v", g.tokenCalls)
	}
}

func TestListFollowsPageTokens(t *testing.T) {
	g := newGoogle(t)
	p := newProvider(t, g, refreshToken)

	first, err := p.List(context.Background(), mail.ListQuery{Query: "in:inbox", Max: 2})
	if err != nil {
		t.Fatal(err)
	}
	q := g.lastQuery
	second, err := p.List(context.Background(), mail.ListQuery{PageToken: first.NextPageToken})

	if len(first.IDs) != 2 || first.NextPageToken != "p2" || q.Get("q") != "in:inbox" || q.Get("maxResults") != "2" {
		t.Fatalf("first %+v, query %v", first, q)
	}
	if err != nil || len(second.IDs) != 1 || second.IDs[0] != "m3" || second.NextPageToken != "" {
		t.Fatalf("second %+v, %v", second, err)
	}
}

func TestGetReadsHeadersAndTellsInboundFromSent(t *testing.T) {
	g := newGoogle(t)
	p := newProvider(t, g, refreshToken)

	in, err := p.Get(context.Background(), "m1")
	if err != nil {
		t.Fatal(err)
	}
	out, err := p.Get(context.Background(), "m-sent")

	if in.Direction != mail.Inbound || in.From != "hr@lumen.example" || len(in.To) != 2 || in.To[1] != "o@x.example" ||
		in.Subject != "Thanks for applying" || in.Snippet == "" || !in.ReceivedAt.Equal(time.UnixMilli(1790000000000)) || in.DraftID != "" {
		t.Fatalf("inbound %+v", in)
	}
	if err != nil || out.Direction != mail.Outbound || out.DraftID != "draft-7" {
		t.Fatalf("sent %+v, %v", out, err)
	}
	if got := g.lastQuery["metadataHeaders"]; len(got) != 4 || g.lastQuery.Get("format") != "metadata" {
		t.Fatalf("asked for %v; want metadata format with four headers", g.lastQuery)
	}
}

func TestGetOfAMissingMessageIsNotFound(t *testing.T) {
	g := newGoogle(t)

	_, err := newProvider(t, g, refreshToken).Get(context.Background(), "nope")

	if !errors.Is(err, mail.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

func TestHistoryCollectsAddedMessagesAcrossPagesOnce(t *testing.T) {
	g := newGoogle(t)
	g.historyBody = `{"history":[{"messagesAdded":[{"message":{"id":"a"}},{"message":{"id":"b"}}]},{"messagesAdded":[{"message":{"id":"a"}}]},{"labelsAdded":[]}],"historyId":"950"}`

	h, err := newProvider(t, g, refreshToken).History(context.Background(), "900")

	if err != nil || len(h.AddedIDs) != 2 || h.AddedIDs[0] != "a" || h.AddedIDs[1] != "b" || h.LatestID != "950" {
		t.Fatalf("got %+v, %v", h, err)
	}
	if g.lastQuery.Get("startHistoryId") != "900" || g.lastQuery.Get("historyTypes") != "messageAdded" {
		t.Fatalf("query %v", g.lastQuery)
	}
}

func TestHistoryWithNothingNewKeepsTheCursor(t *testing.T) {
	g := newGoogle(t)
	g.historyBody = `{"historyId":"900"}`

	h, err := newProvider(t, g, refreshToken).History(context.Background(), "900")

	if err != nil || len(h.AddedIDs) != 0 || h.LatestID != "900" {
		t.Fatalf("got %+v, %v", h, err)
	}
}

func TestHistoryWithAnExpiredCursorSaysSo(t *testing.T) {
	g := newGoogle(t) // answers 404 for history by default

	_, err := newProvider(t, g, refreshToken).History(context.Background(), "1")

	if !errors.Is(err, mail.ErrHistoryExpired) {
		t.Fatalf("got %v, want ErrHistoryExpired", err)
	}
}

func TestSendPostsWellFormedMailWithTheDraftHeader(t *testing.T) {
	g := newGoogle(t)

	sent, err := newProvider(t, g, refreshToken).Send(context.Background(), mail.Outgoing{
		From: "me@example.com", To: []string{"jobs@lumen.example"}, Subject: "Hello", Body: "Dear team", DraftID: "draft-1",
	})

	if err != nil || sent.ID != "sent-1" || sent.ThreadID != "t-sent" {
		t.Fatalf("got %+v, %v", sent, err)
	}
	raw, err := base64.RawURLEncoding.DecodeString(g.sent["raw"].(string))
	if err != nil {
		t.Fatalf("raw is not base64url: %v", err)
	}
	msg, err := netmail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil || msg.Header.Get(mail.DraftHeader) != "draft-1" || msg.Header.Get("To") != "jobs@lumen.example" {
		t.Fatalf("posted mail: %v, headers %v", err, msg.Header)
	}
}

func TestSendRefusesBadMessageWithoutCallingGmail(t *testing.T) {
	g := newGoogle(t)

	_, err := newProvider(t, g, refreshToken).Send(context.Background(), mail.Outgoing{
		From: "me@example.com", To: []string{"a@b.example"}, Subject: "hi\r\nBcc: x@y.example", Body: "b",
	})

	if !errors.Is(err, ErrInvalidMessage) || len(g.tokenCalls) != 0 || g.sent != nil {
		t.Fatalf("got %v; token calls %d, sent %v", err, len(g.tokenCalls), g.sent)
	}
}

func TestARevokedOrUnknownTokenMeansReauth(t *testing.T) {
	tests := []struct {
		name  string
		token string
		setup func(g *google)
	}{
		{"revoked grant", refreshToken, func(g *google) { g.revoked = true }},
		{"unknown refresh token", "1//someone-elses", func(*google) {}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newGoogle(t)
			tt.setup(g)

			_, err := newProvider(t, g, tt.token).Profile(context.Background())

			if !errors.Is(err, mail.ErrAuthRevoked) {
				t.Fatalf("got %v, want ErrAuthRevoked", err)
			}
		})
	}
}

func TestErrorsNeverCarryTokensOrProviderBodies(t *testing.T) {
	g := newGoogle(t)
	g.apiStatus = http.StatusTooManyRequests
	p := newProvider(t, g, refreshToken)

	_, listErr := p.List(context.Background(), mail.ListQuery{})
	_, getErr := p.Get(context.Background(), "m1")
	g.revoked = true
	_, authErr := newProvider(t, g, refreshToken).Profile(context.Background())

	for _, err := range []error{listErr, getErr, authErr} {
		if err == nil {
			t.Fatal("want an error")
		}
		for _, secret := range []string{refreshToken, accessToken, "refresh-secret", "access-secret"} {
			if strings.Contains(err.Error(), secret) {
				t.Fatalf("error %q leaks %q", err, secret)
			}
		}
	}
	var api *mail.APIError
	if !errors.As(listErr, &api) || api.Status != http.StatusTooManyRequests || api.Message != "RESOURCE_EXHAUSTED" {
		t.Fatalf("list error %v, want an APIError with the status", listErr)
	}
}

func TestAuthURLAsksForOfflineAccessWithPKCEAndOnlyReadAndSend(t *testing.T) {
	c, _ := New(Config{ClientID: "id", ClientSecret: "s", RedirectURL: "https://app.example/cb"})

	u, err := url.Parse(c.AuthURL("state-1", "verifier-0123456789-0123456789-0123456789-0123456789"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if u.Host != "accounts.google.com" || q.Get("access_type") != "offline" || q.Get("prompt") != "consent" ||
		q.Get("code_challenge_method") != "S256" || q.Get("code_challenge") == "" || q.Get("state") != "state-1" ||
		q.Get("redirect_uri") != "https://app.example/cb" {
		t.Fatalf("got %v", q)
	}
	if strings.Contains(u.String(), "verifier-0123") {
		t.Fatal("the PKCE verifier is in the URL")
	}
	if scope := q.Get("scope"); scope != ScopeRead+" "+ScopeSend {
		t.Fatalf("scope %q", scope)
	}
}

func TestExchangeReturnsTheRefreshToken(t *testing.T) {
	g := newGoogle(t)
	mux := http.NewServeMux()
	var form url.Values
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		if r.PostForm.Get("code") == "no-refresh" {
			_, _ = io.WriteString(w, `{"access_token":"a","token_type":"Bearer"}`)
			return
		}
		if r.PostForm.Get("code") == "bad" {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"a","refresh_token":"`+refreshToken+`","token_type":"Bearer"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	_ = g
	c, _ := New(Config{ClientID: "id", ClientSecret: "s", TokenURL: srv.URL + "/token"})

	got, err := c.Exchange(context.Background(), "good", "verifier")
	_, noRefresh := c.Exchange(context.Background(), "no-refresh", "verifier")
	_, bad := c.Exchange(context.Background(), "bad", "verifier")

	if err != nil || got != refreshToken || form.Get("code_verifier") != "verifier" {
		t.Fatalf("got %q, %v, form %v", got, err, form)
	}
	if noRefresh == nil {
		t.Fatal("a grant without a refresh token must fail")
	}
	if !errors.Is(bad, mail.ErrAuthRevoked) {
		t.Fatalf("a bad code: got %v", bad)
	}
}

func TestNewRequiresClientCredentials(t *testing.T) {
	if _, err := New(Config{ClientID: "id"}); err == nil {
		t.Fatal("want an error without a client secret")
	}
}
