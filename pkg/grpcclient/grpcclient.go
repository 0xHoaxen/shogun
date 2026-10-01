// Package grpcclient dials other Shogun services with retries, a default
// deadline and the caller's identity attached to every call.
package grpcclient

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/0xHoaxen/shogun/pkg/authz"
)

// DefaultTimeout bounds unary calls whose context has no deadline.
const DefaultTimeout = 5 * time.Second

// retryServiceConfig retries only UNAVAILABLE, three attempts in total.
const retryServiceConfig = `{
  "methodConfig": [{
    "name": [{}],
    "retryPolicy": {
      "maxAttempts": 3,
      "initialBackoff": "0.1s",
      "maxBackoff": "1s",
      "backoffMultiplier": 2,
      "retryableStatusCodes": ["UNAVAILABLE"]
    }
  }]
}`

// Signer produces identity tokens; *authz.Authority implements it.
type Signer interface {
	Sign(id authz.Identity) (string, error)
}

type settings struct {
	signer      Signer
	timeout     time.Duration
	dialOptions []grpc.DialOption
}

// Option customises Dial. Options are pure: they return a modified copy.
type Option func(settings) settings

// WithSigner attaches the identity found in each call's context, signed by s.
// Calls whose context has no identity are sent without a token and will be
// rejected by the server.
func WithSigner(s Signer) Option {
	return func(c settings) settings {
		c.signer = s
		return c
	}
}

// WithTimeout changes the default per-call deadline for unary calls.
func WithTimeout(d time.Duration) Option {
	return func(c settings) settings {
		c.timeout = d
		return c
	}
}

// WithDialOptions appends raw gRPC dial options, such as a bufconn dialer.
func WithDialOptions(opts ...grpc.DialOption) Option {
	return func(c settings) settings {
		c.dialOptions = append(append([]grpc.DialOption(nil), c.dialOptions...), opts...)
		return c
	}
}

// Dial creates a client connection to target. The connection is lazy: it does
// not block, and ctx is accepted for symmetry with future eager dialing.
func Dial(_ context.Context, target string, opts ...Option) (*grpc.ClientConn, error) {
	s := settings{timeout: DefaultTimeout}
	for _, opt := range opts {
		s = opt(s)
	}

	unary := []grpc.UnaryClientInterceptor{deadlineUnary(s.timeout)}
	stream := []grpc.StreamClientInterceptor{}
	if s.signer != nil {
		unary = append(unary, identityUnary(s.signer))
		stream = append(stream, identityStream(s.signer))
	}
	// TODO(P1.8): add otelgrpc stats handler here for trace propagation.

	dialOpts := []grpc.DialOption{
		// Plaintext is acceptable only inside the cluster network for now;
		// P10.3 replaces this with mTLS.
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultServiceConfig(retryServiceConfig),
		grpc.WithChainUnaryInterceptor(unary...),
		grpc.WithChainStreamInterceptor(stream...),
	}
	dialOpts = append(dialOpts, s.dialOptions...)

	conn, err := grpc.NewClient(target, dialOpts...)
	if err != nil {
		return nil, fmt.Errorf("grpcclient: dial %s: %w", target, err)
	}
	return conn, nil
}
