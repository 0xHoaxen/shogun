package authz

import (
	"context"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// exemptPrefixes are methods callable without identity: probes and reflection.
var exemptPrefixes = []string{
	"/grpc.health.v1.Health/",
	"/grpc.reflection.v1.ServerReflection/",
	"/grpc.reflection.v1alpha.ServerReflection/",
}

func exempt(method string) bool {
	for _, p := range exemptPrefixes {
		if strings.HasPrefix(method, p) {
			return true
		}
	}
	return false
}

// authenticate returns ctx with the verified identity, or Unauthenticated.
// The error never says why, so callers learn nothing about the key or claims.
func (a *Authority) authenticate(ctx context.Context) (context.Context, error) {
	md, _ := metadata.FromIncomingContext(ctx)
	vals := md.Get(Header)
	if len(vals) != 1 {
		return ctx, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	id, err := a.Verify(vals[0])
	if err != nil {
		return ctx, status.Error(codes.Unauthenticated, "unauthenticated")
	}
	return WithIdentity(ctx, id), nil
}

// UnaryServerInterceptor rejects calls without a valid identity token.
func (a *Authority) UnaryServerInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		if exempt(info.FullMethod) {
			return next(ctx, req)
		}
		authed, err := a.authenticate(ctx)
		if err != nil {
			return nil, err
		}
		return next(authed, req)
	}
}

// StreamServerInterceptor is the streaming counterpart of UnaryServerInterceptor.
func (a *Authority) StreamServerInterceptor() grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, next grpc.StreamHandler) error {
		if exempt(info.FullMethod) {
			return next(srv, ss)
		}
		authed, err := a.authenticate(ss.Context())
		if err != nil {
			return err
		}
		return next(srv, &identityStream{ServerStream: ss, ctx: authed})
	}
}

type identityStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (s *identityStream) Context() context.Context { return s.ctx }
