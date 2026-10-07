// Package github reads the owner's repositories and merged pull requests from
// the GitHub REST API.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
)

const (
	// DefaultBaseURL is the public GitHub API.
	DefaultBaseURL = "https://api.github.com"

	reposPerPage   = 100
	prsPerPage     = 50
	maxBodyBytes   = 8 << 20
	requestTimeout = 30 * time.Second
	apiVersion     = "2022-11-28"
	// defaultResetWait is how long to wait when GitHub does not say.
	defaultResetWait = time.Minute
)

// Client reads one user's public activity. It never logs or returns the token.
type Client struct {
	baseURL string
	user    string
	token   string
	http    *http.Client
}

// New returns a Client for user. baseURL empty means the public API; a nil
// httpClient means a client with a timeout.
func New(baseURL, user, token string, httpClient *http.Client) *Client {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if httpClient == nil {
		httpClient = &http.Client{Timeout: requestTimeout}
	}
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), user: user, token: token, http: httpClient}
}

type repoJSON struct {
	Name     string    `json:"name"`
	HTMLURL  string    `json:"html_url"`
	Language *string   `json:"language"`
	Stars    int       `json:"stargazers_count"`
	PushedAt time.Time `json:"pushed_at"`
	Fork     bool      `json:"fork"`
	Archived bool      `json:"archived"`
}

type searchJSON struct {
	Items []struct {
		Title         string `json:"title"`
		HTMLURL       string `json:"html_url"`
		RepositoryURL string `json:"repository_url"`
		PullRequest   struct {
			MergedAt *time.Time `json:"merged_at"`
		} `json:"pull_request"`
	} `json:"items"`
}

// Fetch reads the user's repositories and merged pull requests. etag is the one
// from the last snapshot; when the repository list has not changed since, Fetch
// returns domain.ErrNotModified and reads nothing else.
func (c *Client) Fetch(ctx context.Context, etag string) (domain.Snapshot, error) {
	var repos []repoJSON
	newETag, err := c.get(ctx, "/users/"+url.PathEscape(c.user)+"/repos",
		url.Values{"type": {"owner"}, "sort": {"pushed"}, "per_page": {strconv.Itoa(reposPerPage)}}, etag, &repos)
	if err != nil {
		return domain.Snapshot{}, err
	}
	var found searchJSON
	if _, err := c.get(ctx, "/search/issues", url.Values{
		"q":        {fmt.Sprintf("author:%s type:pr is:merged", c.user)},
		"sort":     {"updated"},
		"per_page": {strconv.Itoa(prsPerPage)},
	}, "", &found); err != nil {
		return domain.Snapshot{}, err
	}

	snap := domain.Snapshot{ETag: newETag, Repos: make([]domain.Repo, 0, len(repos))}
	for _, r := range repos {
		if r.Fork || r.Archived {
			continue
		}
		lang := ""
		if r.Language != nil {
			lang = *r.Language
		}
		snap.Repos = append(snap.Repos, domain.Repo{Name: r.Name, URL: r.HTMLURL, Language: lang, Stars: r.Stars, PushedAt: r.PushedAt})
	}
	snap.Contributions.MergedPRs = make([]domain.PullRequest, 0, len(found.Items))
	for _, it := range found.Items {
		if it.PullRequest.MergedAt == nil {
			continue
		}
		snap.Contributions.MergedPRs = append(snap.Contributions.MergedPRs, domain.PullRequest{
			Repo: repoName(it.RepositoryURL), Title: it.Title, URL: it.HTMLURL, MergedAt: *it.PullRequest.MergedAt,
		})
	}
	return snap, nil
}

// repoName turns https://api.github.com/repos/owner/name into owner/name.
func repoName(repositoryURL string) string {
	_, name, _ := strings.Cut(repositoryURL, "/repos/")
	return name
}

// get reads path into out and returns the response's ETag. Errors carry the
// status and never the request, so the token cannot leak through them.
func (c *Client) get(ctx context.Context, path string, query url.Values, etag string, out any) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+query.Encode(), nil)
	if err != nil {
		return "", fmt.Errorf("github: build request: %w", domain.ErrUnavailable)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", "shogun-katana")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		// A failed request's error text carries the URL; the cause adds nothing
		// the owner or the log needs.
		return "", fmt.Errorf("github: request failed: %w", domain.ErrUnavailable)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotModified:
		return "", domain.ErrNotModified
	case resp.StatusCode == http.StatusUnauthorized:
		return "", domain.ErrUnauthorized
	case isRateLimited(resp):
		return "", &domain.RateLimitError{ResetAt: resetAt(resp)}
	case resp.StatusCode == http.StatusForbidden:
		return "", domain.ErrUnauthorized
	case resp.StatusCode != http.StatusOK:
		return "", fmt.Errorf("github: status %d: %w", resp.StatusCode, domain.ErrUnavailable)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxBodyBytes)).Decode(out); err != nil {
		return "", fmt.Errorf("github: decode response: %w", domain.ErrUnavailable)
	}
	return resp.Header.Get("ETag"), nil
}

func isRateLimited(resp *http.Response) bool {
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	return resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0"
}

// resetAt reads when the limit lifts.
func resetAt(resp *http.Response) time.Time {
	if secs, err := strconv.ParseInt(resp.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil {
		return time.Unix(secs, 0)
	}
	if secs, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil {
		return time.Now().Add(time.Duration(secs) * time.Second)
	}
	return time.Now().Add(defaultResetWait)
}
