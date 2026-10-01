package grpcclient

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/0xHoaxen/shogun/pkg/authz"
)

// deadlineUnary applies timeout when the caller set no deadline. Streams are
// left alone: a fixed deadline would cut long-lived streams.
func deadlineUnary(timeout time.Duration) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		if _, ok := ctx.Deadline(); !ok && timeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

// withToken signs the identity in ctx and puts it in outgoing metadata. It
// returns ctx unchanged when there is no identity. A signing failure is
// Internal, so a call never goes out half-authenticated.
func withToken(ctx context.Context, signer Signer) (context.Context, error) {
	id, ok := authz.FromContext(ctx)
	if !ok {
		return ctx, nil
	}
	token, err := signer.Sign(id)
	if err != nil {
		return ctx, status.Error(codes.Internal, fmt.Sprintf("sign identity: %v", err))
	}
	md, _ := metadata.FromOutgoingContext(ctx)
	md = md.Copy()
	md.Set(authz.Header, token)
	return metadata.NewOutgoingContext(ctx, md), nil
}

func identityUnary(signer Signer) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		signed, err := withToken(ctx, signer)
		if err != nil {
			return err
		}
		return invoker(signed, method, req, reply, cc, opts...)
	}
}

func identityStream(signer Signer) grpc.StreamClientInterceptor {
	return func(ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn, method string, streamer grpc.Streamer, opts ...grpc.CallOption) (grpc.ClientStream, error) {
		signed, err := withToken(ctx, signer)
		if err != nil {
			return nil, err
		}
		return streamer(signed, desc, cc, method, opts...)
	}
}
