package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"syscall"
	"time"

	"github.com/0xHoaxen/shogun/services/shinobi/internal/domain"
)

const (
	// UserAgent names shinobi to the sources it reads, and to their robots.txt.
	UserAgent = "shogun-shinobi"

	// MaxBodyBytes caps how much of a source's answer is read.
	MaxBodyBytes = 5 << 20

	maxRobotsBytes  = 512 << 10
	maxRedirects    = 3
	requestTimeout  = 30 * time.Second
	dialTimeout     = 10 * time.Second
	headerTimeout   = 20 * time.Second
	robotsPath      = "/robots.txt"
	httpsScheme     = "https"
	statusClassMask = 100
)

// Errors a fetch can return. Each stands for one thing the owner can act on.
var (
	// ErrRobotsDisallowed means the source's robots.txt forbids reading the address.
	ErrRobotsDisallowed = errors.New("fetch: robots.txt disallows this address")
	// ErrNotPublic means the address names this machine or a private network.
	ErrNotPublic = errors.New("fetch: address is not public")
	// ErrTooLarge means the answer is bigger than MaxBodyBytes.
	ErrTooLarge = errors.New("fetch: answer is too large")
	// ErrFailed means the source could not be read: unreachable, or an error status.
	ErrFailed = errors.New("fetch: source could not be read")
)

// Option changes how a Client reads.
type Option func(*Client)

// WithInterval sets the least time between two requests to one host.
func WithInterval(d time.Duration) Option { return func(c *Client) { c.throttle.interval = d } }

// WithHTTPClient replaces the HTTP client. For tests: it also allows plain
// http, since the guard against private addresses is part of the default
// client's dialer, which this replaces.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http, c.allowHTTP = h, true }
}

// Client fetches sources. It reads only https addresses that resolve to public
// IPs, obeys robots.txt and spaces its requests to one host.
type Client struct {
	http      *http.Client
	throttle  *throttle
	allowHTTP bool
}

// New returns a Client with the guarded transport.
func New(opts ...Option) *Client {
	c := &Client{throttle: newThrottle(DefaultInterval)}
	c.http = &http.Client{
		Timeout:   requestTimeout,
		Transport: guardedTransport(),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) > maxRedirects {
				return fmt.Errorf("%w: too many redirects", ErrFailed)
			}
			if req.URL.Scheme != httpsScheme && !c.allowHTTP {
				return fmt.Errorf("%w: redirect to a non-https address", ErrFailed)
			}
			return nil
		},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// guardedTransport refuses, when connecting, any address that is not public. It
// checks the address actually dialled, after DNS, so a name that points inside
// the network is refused too, and so is every redirect. It ignores proxy
// settings from the environment.
func guardedTransport() *http.Transport {
	dialer := &net.Dialer{
		Timeout: dialTimeout,
		Control: func(_, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return ErrNotPublic
			}
			ip, err := netip.ParseAddr(host)
			if err != nil || !domain.IsPublicIP(ip) {
				return ErrNotPublic
			}
			return nil
		},
	}
	return &http.Transport{
		Proxy:                 nil,
		DialContext:           dialer.DialContext,
		TLSHandshakeTimeout:   dialTimeout,
		ResponseHeaderTimeout: headerTimeout,
		MaxIdleConns:          10,
		IdleConnTimeout:       time.Minute,
	}
}

// Get returns the body of rawURL, after checking the host's robots.txt.
func (c *Client) Get(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || (u.Scheme != httpsScheme && (!c.allowHTTP || u.Scheme != "http")) {
		return nil, fmt.Errorf("%w: not an https address", ErrFailed)
	}
	if err := c.checkRobots(ctx, u); err != nil {
		return nil, err
	}
	return c.fetch(ctx, u.String(), MaxBodyBytes)
}

// checkRobots reads the host's robots.txt and refuses a path it forbids. A
// missing file allows everything; a server error forbids everything, as RFC
// 9309 asks.
func (c *Client) checkRobots(ctx context.Context, u *url.URL) error {
	robotsURL := &url.URL{Scheme: u.Scheme, Host: u.Host, Path: robotsPath}
	body, err := c.fetch(ctx, robotsURL.String(), maxRobotsBytes)
	var status *statusError
	switch {
	case err == nil:
		path := u.EscapedPath()
		if !parseRobots(body, UserAgent).allowed(path) {
			return ErrRobotsDisallowed
		}
		return nil
	case errors.As(err, &status) && status.code/statusClassMask == 4:
		return nil
	case errors.As(err, &status):
		return ErrRobotsDisallowed
	default:
		return err
	}
}

// statusError is an answer with an error status.
type statusError struct{ code int }

func (e *statusError) Error() string { return fmt.Sprintf("%s: status %d", ErrFailed, e.code) }

// Unwrap makes an error status a failure to read the source.
func (e *statusError) Unwrap() error { return ErrFailed }

func (c *Client) fetch(ctx context.Context, rawURL string, limit int64) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("%w: bad address", ErrFailed)
	}
	if err := c.throttle.wait(ctx, u.Host); err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: bad request", ErrFailed)
	}
	req.Header.Set("User-Agent", UserAgent)
	req.Header.Set("Accept", "application/json, application/rss+xml, application/atom+xml, application/xml, text/xml, */*;q=0.5")
	resp, err := c.http.Do(req)
	if err != nil {
		if errors.Is(err, ErrNotPublic) {
			return nil, ErrNotPublic
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// The text of a failed request names the address; the cause adds nothing.
		return nil, fmt.Errorf("%w: request failed", ErrFailed)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, &statusError{code: resp.StatusCode}
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("%w: read failed", ErrFailed)
	}
	if int64(len(body)) > limit {
		return nil, ErrTooLarge
	}
	return body, nil
}
