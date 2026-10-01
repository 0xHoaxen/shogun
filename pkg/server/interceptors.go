package server

import (
	"context"
	"log/slog"
	"runtime/debug"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// recoverUnary turns a handler panic into codes.Internal and logs it.
func recoverUnary(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = recovered(ctx, log, info.FullMethod, r)
			}
		}()
		return next(ctx, req)
	}
}

// recoverStream is the streaming counterpart of recoverUnary.
func recoverStream(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, next grpc.StreamHandler) (err error) {
		defer func() {
			if r := recover(); r != nil {
				err = recovered(ss.Context(), log, info.FullMethod, r)
			}
		}()
		return next(srv, ss)
	}
}

func recovered(ctx context.Context, log *slog.Logger, method string, r any) error {
	log.ErrorContext(ctx, "grpc handler panic",
		slog.String("method", method),
		slog.Any("panic", r),
		slog.String("stack", string(debug.Stack())),
	)
	return status.Error(codes.Internal, "internal error")
}

// logUnary logs method, code and duration of each call, never the payload.
func logUnary(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, next grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := next(ctx, req)
		logCall(ctx, log, info.FullMethod, err, start)
		return resp, err
	}
}

// logStream is the streaming counterpart of logUnary.
func logStream(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, next grpc.StreamHandler) error {
		start := time.Now()
		err := next(srv, ss)
		logCall(ss.Context(), log, info.FullMethod, err, start)
		return err
	}
}

func logCall(ctx context.Context, log *slog.Logger, method string, err error, start time.Time) {
	code := status.Code(err)
	level := slog.LevelInfo
	if code == codes.Internal || code == codes.Unknown || code == codes.DataLoss {
		level = slog.LevelError
	}
	log.LogAttrs(ctx, level, "grpc call",
		slog.String("method", method),
		slog.String("code", code.String()),
		slog.Duration("duration", time.Since(start)),
	)
}
