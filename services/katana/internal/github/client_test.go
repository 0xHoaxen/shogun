package github_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
	"github.com/0xHoaxen/shogun/services/katana/internal/github"
)

const token = "ghp_secret-token-value"

const reposBody = `[
 {"name":"shogun","html_url":"https://github.com/o/shogun","language":"Go","stargazers_count":7,"pushed_at":"2026-10-06T10:00:00Z","fork":false,"archived":false},
 {"name":"forked","html_url":"https://github.com/o/forked","language":"C","stargazers_count":1,"pushed_at":"2026-10-01T10:00:00Z","fork":true,"archived":false},
 {"name":"old","html_url":"https://github.com/o/old","language":null,"stargazers_count":0,"pushed_at":"2020-01-01T00:00:00Z","fork":false,"archived":true},
 {"name":"notes","html_url":"https://github.com/o/notes","language":null,"stargazers_count":0,"pushed_at":"2026-09-01T00:00:00Z","fork":false,"archived":false}
]`

const searchBody = `{"items":[
 {"title":"Add worker pool","html_url":"https://github.com/x/y/pull/12","repository_url":"https://api.github.com/repos/x/y","pull_request":{"merged_at":"2026-10-05T09:00:00Z"}},
 {"title":"Closed unmerged","html_url":"https://github.com/x/y/pull/13","repository_url":"https://api.github.com/repos/x/y","pull_request":{"merged_at":null}}
]}`

// fakeGitHub serves the repo list and the search. reposCode, when not 200,
// answers the repo list with that code and reposHdr; searchSts does so for the
// search.
type fakeGitHub struct {
	etag      string
	reposCode int
	reposHdr  map[string]string
	searchSts int
	seen      []*http.Request
}

func (f *fakeGitHub) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.seen = append(f.seen, r.Clone(r.Context()))
		switch {
		case strings.HasPrefix(r.URL.Path, "/users/octo/repos"):
			if f.reposCode != 0 && f.reposCode != http.StatusOK {
				for k, v := range f.reposHdr {
					w.Header().Set(k, v)
				}
				w.WriteHeader(f.reposCode)
				return
			}
			if r.Header.Get("If-None-Match") == f.etag {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			w.Header().Set("ETag", f.etag)
			_, _ = w.Write([]byte(reposBody))
		case r.URL.Path == "/search/issues":
			if f.searchSts != 0 {
				w.WriteHeader(f.searchSts)
				return
			}
			_, _ = w.Write([]byte(searchBody))
		default:
			http.NotFound(w, r)
		}
	})
}

func TestFetchReadsOwnReposAndMergedPullRequests(t *testing.T) {
	fake := &fakeGitHub{etag: `W/"v1"`}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	snap, err := github.New(srv.URL, "octo", token, nil).Fetch(context.Background(), "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(snap.Repos) != 2 || snap.Repos[0].Name != "shogun" || snap.Repos[0].Language != "Go" || snap.Repos[0].Stars != 7 || snap.Repos[1].Language != "" {
		t.Fatalf("repos = %+v, want the two original, unarchived ones", snap.Repos)
	}
	if got := snap.Contributions.MergedPRs; len(got) != 1 || got[0].Repo != "x/y" || got[0].Title != "Add worker pool" || got[0].MergedAt.IsZero() {
		t.Fatalf("merged prs = %+v, want only the merged one", got)
	}
	if snap.ETag != `W/"v1"` {
		t.Fatalf("etag = %q", snap.ETag)
	}
	for _, r := range fake.seen {
		if r.Header.Get("Authorization") != "Bearer "+token || r.Header.Get("User-Agent") == "" {
			t.Fatalf("request to %s lacks auth or user agent", r.URL.Path)
		}
	}
	if q := fake.seen[1].URL.Query().Get("q"); q != "author:octo type:pr is:merged" {
		t.Fatalf("search query = %q", q)
	}
}

func TestFetchSendsTheETagAndReadsNothingMoreOn304(t *testing.T) {
	fake := &fakeGitHub{etag: `W/"v1"`}
	srv := httptest.NewServer(fake.handler())
	defer srv.Close()

	_, err := github.New(srv.URL, "octo", token, nil).Fetch(context.Background(), `W/"v1"`)

	if !errors.Is(err, domain.ErrNotModified) {
		t.Fatalf("err = %v, want ErrNotModified", err)
	}
	if len(fake.seen) != 1 || fake.seen[0].Header.Get("If-None-Match") != `W/"v1"` {
		t.Fatalf("requests = %d, want only the conditional repo list", len(fake.seen))
	}
}

func TestFetchMapsGitHubFailures(t *testing.T) {
	reset := time.Now().Add(10 * time.Minute).Round(time.Second)
	tests := []struct {
		name  string
		fake  *fakeGitHub
		check func(error) bool
	}{
		{"bad token", &fakeGitHub{reposCode: http.StatusUnauthorized}, func(e error) bool { return errors.Is(e, domain.ErrUnauthorized) }},
		{"forbidden", &fakeGitHub{reposCode: http.StatusForbidden}, func(e error) bool { return errors.Is(e, domain.ErrUnauthorized) }},
		{"rate limited", &fakeGitHub{reposCode: http.StatusForbidden, reposHdr: map[string]string{
			"X-RateLimit-Remaining": "0", "X-RateLimit-Reset": strconv.FormatInt(reset.Unix(), 10),
		}}, func(e error) bool {
			var rl *domain.RateLimitError
			return errors.As(e, &rl) && rl.ResetAt.Unix() == reset.Unix()
		}},
		{"too many requests", &fakeGitHub{reposCode: http.StatusTooManyRequests}, func(e error) bool {
			var rl *domain.RateLimitError
			return errors.As(e, &rl) && rl.ResetAt.After(time.Now())
		}},
		{"server error", &fakeGitHub{reposCode: http.StatusBadGateway}, func(e error) bool { return errors.Is(e, domain.ErrUnavailable) }},
		{"search fails", &fakeGitHub{etag: "e", searchSts: http.StatusInternalServerError}, func(e error) bool { return errors.Is(e, domain.ErrUnavailable) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(tt.fake.handler())
			defer srv.Close()

			_, err := github.New(srv.URL, "octo", token, nil).Fetch(context.Background(), "")

			if err == nil || !tt.check(err) || strings.Contains(err.Error(), token) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestFetchErrorsNeverContainTheToken(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close() // nothing listens any more

	_, err := github.New(url, "octo", token, nil).Fetch(context.Background(), "")

	if !errors.Is(err, domain.ErrUnavailable) || strings.Contains(err.Error(), token) {
		t.Fatalf("err = %v", err)
	}
}
