package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/pkg/grpcclient"
	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/settings"
	"github.com/0xHoaxen/shogun/services/torii/internal/store"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/connectapi"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/httpauth"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/httpmw"
)

const (
	// Paths are relative so they resolve against whichever origin the browser
	// reached torii through, the web app's proxy.
	loginPath     = "/login"
	postLoginPath = "/jobs"

	publicReadHeaderTimeout = 10 * time.Second
	publicIdleTimeout       = 2 * time.Minute
)

// newPublicServer builds the browser-facing HTTP server: the /auth/* login
// routes and the ConnectRPC API, behind a per-client rate limit. signer signs
// the owner's identity onto calls to kagami. The returned function releases
// the connection to kagami.
func newPublicServer(
	ctx context.Context,
	s settings.Settings,
	pool *pgxpool.Pool,
	identityKey []byte,
	signer grpcclient.Signer,
	log *slog.Logger,
) (*http.Server, func(), error) {
	provider, err := httpauth.NewOIDCProvider(ctx, httpauth.ProviderConfig{
		IssuerURL:    s.GoogleIssuerURL,
		ClientID:     s.GoogleClientID,
		ClientSecret: s.GoogleClientSecret,
		RedirectURL:  s.RedirectURL(),
	})
	if err != nil {
		return nil, nil, err
	}
	auth, err := app.NewAuth(store.NewSessions(pool), app.AuthConfig{
		AllowedEmails: s.AllowedEmails,
		SessionTTL:    s.SessionTTL,
	})
	if err != nil {
		return nil, nil, err
	}
	secure := s.SecureCookies()
	login, err := httpauth.NewHandler(provider, auth, httpauth.Config{
		LoginURL:      loginPath,
		PostLoginURL:  postLoginPath,
		SecureCookies: secure,
		FlowKey:       httpauth.DeriveFlowKey(identityKey),
	}, log)
	if err != nil {
		return nil, nil, err
	}
	kagamiConn, err := grpcclient.Dial(ctx, s.KagamiAddr, grpcclient.WithSigner(signer))
	if err != nil {
		return nil, nil, fmt.Errorf("dial kagami: %w", err)
	}
	kagami := kagamiv1.NewKagamiServiceClient(kagamiConn)
	sorobanConn, err := grpcclient.Dial(ctx, s.SorobanAddr, grpcclient.WithSigner(signer))
	if err != nil {
		_ = kagamiConn.Close()
		return nil, nil, fmt.Errorf("dial soroban: %w", err)
	}
	soroban := sorobanv1.NewSorobanServiceClient(sorobanConn)

	interceptor := connectapi.NewSessionInterceptor(connectapi.InterceptorConfig{
		Auth:          auth,
		SecureCookies: secure,
		Open:          []string{apiv1connect.AuthServiceLogoutProcedure},
		Log:           log,
	})
	handlerOpts := []connect.HandlerOption{
		connect.WithInterceptors(interceptor),
		connect.WithReadMaxBytes(s.MaxBodyBytes),
	}
	mux := http.NewServeMux()
	mux.Handle("/auth/", httpmw.LimitBody(int64(s.MaxBodyBytes))(login.Routes()))
	mux.Handle(apiv1connect.NewAuthServiceHandler(connectapi.NewAuthServer(auth, secure, log), handlerOpts...))
	mux.Handle(apiv1connect.NewJobsServiceHandler(connectapi.NewJobsServer(kagami, log), handlerOpts...))
	mux.Handle(apiv1connect.NewContactsServiceHandler(connectapi.NewContactsServer(kagami, log), handlerOpts...))
	mux.Handle(apiv1connect.NewCostsServiceHandler(connectapi.NewCostsServer(soroban, log), handlerOpts...))

	srv := &http.Server{
		Handler:           httpmw.NewRateLimiter(s.RateLimit, s.RateBurst).Middleware(mux),
		ReadHeaderTimeout: publicReadHeaderTimeout,
		IdleTimeout:       publicIdleTimeout,
	}
	closeBackends := func() {
		for name, conn := range map[string]interface{ Close() error }{"kagami": kagamiConn, "soroban": sorobanConn} {
			if err := conn.Close(); err != nil {
				log.Warn("close backend connection", slog.String("backend", name), slog.Any("error", err))
			}
		}
	}
	return srv, closeBackends, nil
}

// servePublic serves srv on lis in the background. A failure other than a
// requested shutdown calls onFailure. The returned function shuts the server
// down, waiting at most timeout for requests in flight.
func servePublic(srv *http.Server, lis net.Listener, log *slog.Logger, onFailure func()) func(timeout time.Duration) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		log.Info("public listener started", slog.String("addr", lis.Addr().String()))
		if err := srv.Serve(lis); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("public listener failed", slog.Any("error", err))
			onFailure()
		}
	}()
	return func(timeout time.Duration) {
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil {
			log.Error("public listener shutdown", slog.Any("error", err))
		}
		<-done
	}
}
