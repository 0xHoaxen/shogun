// Package gmail implements mail.Provider over Gmail's REST API. It uses
// net/http and golang.org/x/oauth2 directly: the four calls tsubame makes are
// small, and the official client needs a newer Go than the repository pins.
package gmail

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail"
)

// Google endpoints and the scopes tsubame asks for: read mail to sync it, and
// send mail the owner approved. It cannot change or delete anything.
const (
	DefaultAPIBase  = "https://gmail.googleapis.com"
	DefaultAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	DefaultTokenURL = "https://oauth2.googleapis.com/token"

	ScopeRead = "https://www.googleapis.com/auth/gmail.readonly"
	ScopeSend = "https://www.googleapis.com/auth/gmail.send"

	requestTimeout = 30 * time.Second
	// maxErrorBody bounds how much of an error answer is read; only Google's
	// short message is kept from it.
	maxErrorBody = 4 << 10
	// maxBody bounds a successful answer.
	maxBody = 8 << 20

	sentLabel = "SENT"
)

// Config configures the client. Empty URLs mean Google's; tests point them at
// a local server.
type Config struct {
	ClientID     string
	ClientSecret string
	RedirectURL  string
	APIBase      string
	AuthURL      string
	TokenURL     string
}

// Client builds a mail.Provider per account. It implements mail.Factory.
type Client struct {
	oauth   oauth2.Config
	apiBase string
}

// New returns a Client.
func New(cfg Config) (*Client, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("gmail: client id and secret are required")
	}
	orDefault := func(v, d string) string {
		if v == "" {
			return d
		}
		return strings.TrimRight(v, "/")
	}
	return &Client{
		apiBase: orDefault(cfg.APIBase, DefaultAPIBase),
		oauth: oauth2.Config{
			ClientID: cfg.ClientID, ClientSecret: cfg.ClientSecret, RedirectURL: cfg.RedirectURL,
			Scopes: []string{ScopeRead, ScopeSend},
			Endpoint: oauth2.Endpoint{
				AuthURL: orDefault(cfg.AuthURL, DefaultAuthURL), TokenURL: orDefault(cfg.TokenURL, DefaultTokenURL),
			},
		},
	}, nil
}

// AuthURL returns where to send the owner to grant access. The PKCE verifier
// stays with the caller; only its challenge goes in the URL. Offline access with
// a forced consent screen makes Google return a refresh token every time.
func (c *Client) AuthURL(state, verifier string) string {
	return c.oauth.AuthCodeURL(state,
		oauth2.AccessTypeOffline, oauth2.SetAuthURLParam("prompt", "consent"), oauth2.S256ChallengeOption(verifier))
}

// Exchange trades an authorization code for the account's refresh token.
func (c *Client) Exchange(ctx context.Context, code, verifier string) (refreshToken string, err error) {
	tok, err := c.oauth.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", classify(err)
	}
	if tok.RefreshToken == "" {
		return "", errors.New("gmail: no refresh token granted")
	}
	return tok.RefreshToken, nil
}

// Provider implements mail.Factory. The returned provider refreshes its access
// token as needed and never exposes either token.
func (c *Client) Provider(refreshToken string) mail.Provider {
	ctx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: requestTimeout})
	return &provider{
		http:    c.oauth.Client(ctx, &oauth2.Token{RefreshToken: refreshToken}),
		apiBase: c.apiBase,
	}
}

type provider struct {
	http    *http.Client
	apiBase string
}

// classify maps an oauth2 failure to mail.ErrAuthRevoked when Google says the
// grant is gone, and otherwise to a plain error without the response body.
func classify(err error) error {
	var re *oauth2.RetrieveError
	if errors.As(err, &re) {
		if re.ErrorCode == "invalid_grant" || re.ErrorCode == "unauthorized_client" {
			return mail.ErrAuthRevoked
		}
		return fmt.Errorf("gmail: token endpoint answered %d %s", re.Response.StatusCode, re.ErrorCode)
	}
	return err
}

// do runs one request and decodes a JSON answer into out (which may be nil).
func (p *provider) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("gmail: encode request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}
	target := p.apiBase + "/gmail/v1/users/me/" + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return fmt.Errorf("gmail: build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := p.http.Do(req)
	if err != nil {
		return classify(redactURL(err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode/100 != 2 {
		return apiError(resp)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBody)).Decode(out); err != nil {
		return fmt.Errorf("gmail: decode answer: %w", err)
	}
	return nil
}

// redactURL drops the request URL from a transport error, which can carry a
// message id or a query.
func redactURL(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) {
		return fmt.Errorf("gmail: %s: %w", ue.Op, ue.Err)
	}
	return err
}

// apiError keeps only Google's status code (such as RESOURCE_EXHAUSTED), a
// fixed vocabulary. The free-text message is dropped: it is not needed to act
// on, and an error string must never be able to carry anything from a request.
func apiError(resp *http.Response) error {
	var payload struct {
		Error struct {
			Status string `json:"status"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, maxErrorBody)).Decode(&payload)
	switch resp.StatusCode {
	case http.StatusUnauthorized:
		return mail.ErrAuthRevoked
	case http.StatusNotFound:
		return mail.ErrNotFound
	}
	return &mail.APIError{Status: resp.StatusCode, Message: payload.Error.Status}
}

// Profile implements mail.Provider.
func (p *provider) Profile(ctx context.Context) (mail.Profile, error) {
	var out struct {
		EmailAddress string `json:"emailAddress"`
		HistoryID    string `json:"historyId"`
	}
	if err := p.do(ctx, http.MethodGet, "profile", nil, nil, &out); err != nil {
		return mail.Profile{}, err
	}
	return mail.Profile{Address: strings.ToLower(out.EmailAddress), HistoryID: out.HistoryID}, nil
}

type messageRef struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
}

// List implements mail.Provider.
func (p *provider) List(ctx context.Context, q mail.ListQuery) (mail.Page, error) {
	query := url.Values{}
	if q.Query != "" {
		query.Set("q", q.Query)
	}
	if q.PageToken != "" {
		query.Set("pageToken", q.PageToken)
	}
	if q.Max > 0 {
		query.Set("maxResults", strconv.Itoa(q.Max))
	}
	var out struct {
		Messages      []messageRef `json:"messages"`
		NextPageToken string       `json:"nextPageToken"`
	}
	if err := p.do(ctx, http.MethodGet, "messages", query, nil, &out); err != nil {
		return mail.Page{}, err
	}
	page := mail.Page{NextPageToken: out.NextPageToken}
	for _, m := range out.Messages {
		page.IDs = append(page.IDs, m.ID)
	}
	return page, nil
}

// History implements mail.Provider. A cursor Gmail no longer has answers 404,
// which is mail.ErrHistoryExpired.
func (p *provider) History(ctx context.Context, since string) (mail.History, error) {
	h := mail.History{LatestID: since}
	seen := map[string]bool{}
	pageToken := ""
	for {
		query := url.Values{"startHistoryId": {since}, "historyTypes": {"messageAdded"}}
		if pageToken != "" {
			query.Set("pageToken", pageToken)
		}
		var out struct {
			History []struct {
				MessagesAdded []struct {
					Message messageRef `json:"message"`
				} `json:"messagesAdded"`
			} `json:"history"`
			HistoryID     string `json:"historyId"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := p.do(ctx, http.MethodGet, "history", query, nil, &out); err != nil {
			if errors.Is(err, mail.ErrNotFound) {
				return mail.History{}, mail.ErrHistoryExpired
			}
			return mail.History{}, err
		}
		for _, rec := range out.History {
			for _, added := range rec.MessagesAdded {
				if id := added.Message.ID; !seen[id] {
					seen[id] = true
					h.AddedIDs = append(h.AddedIDs, id)
				}
			}
		}
		if out.HistoryID != "" {
			h.LatestID = out.HistoryID
		}
		if out.NextPageToken == "" {
			return h, nil
		}
		pageToken = out.NextPageToken
	}
}

// Get implements mail.Provider. Only headers and the snippet are requested.
func (p *provider) Get(ctx context.Context, id string) (mail.Message, error) {
	query := url.Values{"format": {"metadata"}}
	for _, h := range []string{"From", "To", "Subject", mail.DraftHeader} {
		query.Add("metadataHeaders", h)
	}
	var out struct {
		ID           string   `json:"id"`
		ThreadID     string   `json:"threadId"`
		LabelIDs     []string `json:"labelIds"`
		Snippet      string   `json:"snippet"`
		InternalDate string   `json:"internalDate"`
		Payload      struct {
			Headers []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"headers"`
		} `json:"payload"`
	}
	if err := p.do(ctx, http.MethodGet, "messages/"+url.PathEscape(id), query, nil, &out); err != nil {
		return mail.Message{}, err
	}
	headers := map[string]string{}
	for _, h := range out.Payload.Headers {
		headers[strings.ToLower(h.Name)] = h.Value
	}
	ms, _ := strconv.ParseInt(out.InternalDate, 10, 64)
	m := mail.Message{
		ID: out.ID, ThreadID: out.ThreadID, Direction: mail.Inbound,
		From: firstAddress(headers["from"]), To: addresses(headers["to"]), Subject: headers["subject"],
		Snippet: out.Snippet, ReceivedAt: time.UnixMilli(ms).UTC(), DraftID: headers[strings.ToLower(mail.DraftHeader)],
	}
	for _, label := range out.LabelIDs {
		if label == sentLabel {
			m.Direction = mail.Outbound
		}
	}
	return m, nil
}

// Send implements mail.Provider.
func (p *provider) Send(ctx context.Context, m mail.Outgoing) (mail.Sent, error) {
	raw, err := buildMessage(m, time.Now())
	if err != nil {
		return mail.Sent{}, err
	}
	var out messageRef
	body := map[string]string{"raw": base64.RawURLEncoding.EncodeToString(raw)}
	if err := p.do(ctx, http.MethodPost, "messages/send", nil, body, &out); err != nil {
		return mail.Sent{}, err
	}
	return mail.Sent{ID: out.ID, ThreadID: out.ThreadID}, nil
}
