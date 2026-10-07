package fetch_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/services/shinobi/internal/fetch"
)

// site serves robots.txt and one page, and records the requests it saw.
type site struct {
	mu        sync.Mutex
	robots    string
	robotsSts int
	page      string
	pageSts   int
	seen      []string
	agents    []string
	times     []time.Time
}

func (s *site) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.seen = append(s.seen, r.URL.Path)
		s.agents = append(s.agents, r.Header.Get("User-Agent"))
		s.times = append(s.times, time.Now())
		s.mu.Unlock()
		switch r.URL.Path {
		case "/robots.txt":
			if s.robotsSts != 0 {
				w.WriteHeader(s.robotsSts)
				return
			}
			_, _ = w.Write([]byte(s.robots))
		case "/redirect":
			http.Redirect(w, r, "/page", http.StatusFound)
		default:
			if s.pageSts != 0 {
				w.WriteHeader(s.pageSts)
				return
			}
			_, _ = w.Write([]byte(s.page))
		}
	})
}

func newSite(t *testing.T, s *site) (*fetch.Client, string) {
	t.Helper()
	srv := httptest.NewServer(s.handler())
	t.Cleanup(srv.Close)
	return fetch.New(fetch.WithHTTPClient(srv.Client()), fetch.WithInterval(0)), srv.URL
}

func TestGetReadsThePageAfterCheckingRobotsAndIdentifiesItself(t *testing.T) {
	s := &site{robots: "User-agent: *\nDisallow: /private\n", page: "hello"}
	client, base := newSite(t, s)

	body, err := client.Get(context.Background(), base+"/jobs/feed")

	if err != nil || string(body) != "hello" {
		t.Fatalf("body %q, err %v", body, err)
	}
	if len(s.seen) != 2 || s.seen[0] != "/robots.txt" || s.seen[1] != "/jobs/feed" {
		t.Fatalf("requests = %v, want robots first", s.seen)
	}
	for _, a := range s.agents {
		if a != fetch.UserAgent {
			t.Fatalf("user agent = %q", a)
		}
	}
}

func TestGetRespectsRobots(t *testing.T) {
	tests := []struct {
		name    string
		site    *site
		wantErr error
	}{
		{"disallowed", &site{robots: "User-agent: *\nDisallow: /feed\n", page: "x"}, fetch.ErrRobotsDisallowed},
		{"no robots file", &site{robotsSts: http.StatusNotFound, page: "x"}, nil},
		{"robots forbidden", &site{robotsSts: http.StatusForbidden, page: "x"}, nil},
		{"robots server error means no", &site{robotsSts: http.StatusInternalServerError, page: "x"}, fetch.ErrRobotsDisallowed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, base := newSite(t, tt.site)

			_, err := client.Get(context.Background(), base+"/feed")

			if !errors.Is(err, tt.wantErr) && (tt.wantErr != nil || err != nil) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil && len(tt.site.seen) != 1 {
				t.Fatalf("requests = %v; a forbidden page must not be requested", tt.site.seen)
			}
		})
	}
}

func TestGetFailuresAreErrFailedAndErrTooLarge(t *testing.T) {
	notFound, _ := newSite(t, &site{pageSts: http.StatusNotFound})
	_ = notFound
	s404 := &site{pageSts: http.StatusNotFound}
	client404, base404 := newSite(t, s404)
	sBig := &site{page: strings.Repeat("x", fetch.MaxBodyBytes+1)}
	clientBig, baseBig := newSite(t, sBig)

	_, notFoundErr := client404.Get(context.Background(), base404+"/feed")
	_, bigErr := clientBig.Get(context.Background(), baseBig+"/feed")
	_, schemeErr := client404.Get(context.Background(), "ftp://example.com/feed")
	_, hostErr := client404.Get(context.Background(), "https:///feed")

	if !errors.Is(notFoundErr, fetch.ErrFailed) || !errors.Is(bigErr, fetch.ErrTooLarge) ||
		!errors.Is(schemeErr, fetch.ErrFailed) || !errors.Is(hostErr, fetch.ErrFailed) {
		t.Fatalf("errors: %v | %v | %v | %v", notFoundErr, bigErr, schemeErr, hostErr)
	}
}

func TestGetFollowsAFewRedirectsButChecksRobotsOnlyForTheFirstAddress(t *testing.T) {
	s := &site{page: "landed"}
	client, base := newSite(t, s)

	body, err := client.Get(context.Background(), base+"/redirect")

	if err != nil || string(body) != "landed" {
		t.Fatalf("body %q, err %v", body, err)
	}
}

func TestGetSpacesRequestsToOneHost(t *testing.T) {
	s := &site{page: "x"}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()
	const interval = 150 * time.Millisecond
	client := fetch.New(fetch.WithHTTPClient(srv.Client()), fetch.WithInterval(interval))

	if _, err := client.Get(context.Background(), srv.URL+"/a"); err != nil {
		t.Fatalf("get: %v", err)
	}

	for i := 1; i < len(s.times); i++ {
		if gap := s.times[i].Sub(s.times[i-1]); gap < interval-20*time.Millisecond {
			t.Fatalf("request %d came %s after the one before, want at least %s", i, gap, interval)
		}
	}
	if len(s.times) != 2 {
		t.Fatalf("requests = %d, want robots and the page", len(s.times))
	}
}

func TestGetStopsWhenTheContextEnds(t *testing.T) {
	s := &site{page: "x"}
	srv := httptest.NewServer(s.handler())
	defer srv.Close()
	client := fetch.New(fetch.WithHTTPClient(srv.Client()), fetch.WithInterval(time.Minute))
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.Get(ctx, srv.URL+"/a")

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context's deadline while waiting between requests", err)
	}
}

// The default client must refuse private addresses at connect time, whatever
// the URL says. A loopback TLS server stands for an internal service.
func TestTheDefaultClientRefusesToConnectToPrivateAddresses(t *testing.T) {
	var hit bool
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer srv.Close()

	_, err := fetch.New(fetch.WithInterval(0)).Get(context.Background(), srv.URL+"/feed")

	if !errors.Is(err, fetch.ErrNotPublic) || hit {
		t.Fatalf("err = %v, server reached = %v; want ErrNotPublic and no request", err, hit)
	}
}

func TestTheDefaultClientRefusesPlainHTTP(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	_, err := fetch.New().Get(context.Background(), srv.URL+"/feed")

	if !errors.Is(err, fetch.ErrFailed) {
		t.Fatalf("err = %v, want ErrFailed", err)
	}
}
