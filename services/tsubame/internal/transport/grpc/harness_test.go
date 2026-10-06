package grpc_test

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/app"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/envelope"
	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail/gmail"
	tsubamegrpc "github.com/0xHoaxen/shogun/services/tsubame/internal/transport/grpc"
	"github.com/0xHoaxen/shogun/services/tsubame/migrations"
)

func TestMain(m *testing.M) { os.Exit(postgrestest.Run(m)) }

// Values Google's fake hands out, so tests can search the logs for them.
const (
	goodCode     = "4/good-authorization-code"
	refreshToken = "1//granted-refresh-token"
	accessToken  = "ya29.granted-access-token"
	mailbox      = "Me@Example.com"
)

// google fakes Google's token endpoint and Gmail profile. A code works once.
type google struct {
	srv *httptest.Server

	mu        sync.Mutex
	usedCodes map[string]bool
	verifiers []string
}

func newGoogle(t *testing.T) *google {
	t.Helper()
	g := &google{usedCodes: map[string]bool{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		w.Header().Set("Content-Type", "application/json")
		refused := func() {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = io.WriteString(w, `{"error":"invalid_grant"}`)
		}
		if r.PostForm.Get("grant_type") == "refresh_token" {
			if r.PostForm.Get("refresh_token") != refreshToken {
				refused()
				return
			}
			_, _ = io.WriteString(w, `{"access_token":"`+accessToken+`","token_type":"Bearer","expires_in":3600}`)
			return
		}
		g.mu.Lock()
		code := r.PostForm.Get("code")
		used := g.usedCodes[code]
		g.usedCodes[code] = true
		g.verifiers = append(g.verifiers, r.PostForm.Get("code_verifier"))
		g.mu.Unlock()
		if code != goodCode || used {
			refused()
			return
		}
		_, _ = io.WriteString(w, `{"access_token":"`+accessToken+`","refresh_token":"`+refreshToken+`","token_type":"Bearer","expires_in":3600}`)
	})
	mux.HandleFunc("/gmail/v1/users/me/profile", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+accessToken {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, `{"emailAddress":"`+mailbox+`","historyId":"77"}`)
	})
	g.srv = httptest.NewServer(mux)
	t.Cleanup(g.srv.Close)
	return g
}

// harness is a tsubame server on an in-memory listener with the real authz
// interceptors, backed by a migrated schema and the fake Google.
type harness struct {
	client    tsubamev1.TsubameServiceClient
	pool      *pgxpool.Pool
	authority *authz.Authority
	accounts  *app.Accounts
	google    *google
	logs      *bytes.Buffer
	owner     string

	mu  sync.Mutex
	now time.Time
}

func (h *harness) clock() time.Time {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.now
}

func (h *harness) advance(d time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.now = h.now.Add(d)
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "tsubame")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	g := newGoogle(t)
	keys, err := envelope.NewKeyring("k1", map[string][]byte{"k1": bytes.Repeat([]byte{9}, envelope.KeySize)})
	if err != nil {
		t.Fatal(err)
	}
	client, err := gmail.New(gmail.Config{
		ClientID: "id", ClientSecret: "secret", RedirectURL: "https://app.example/mail/callback",
		APIBase: g.srv.URL, TokenURL: g.srv.URL + "/token", AuthURL: "https://accounts.example/auth",
	})
	if err != nil {
		t.Fatal(err)
	}
	h := &harness{pool: pool, google: g, logs: &bytes.Buffer{}, owner: uuid.NewString(), now: time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)}
	log := slog.New(slog.NewTextHandler(&lockedWriter{w: h.logs}, &slog.HandlerOptions{Level: slog.LevelDebug}))
	h.accounts = app.NewAccounts(pool, keys)
	connector := app.NewConnector(h.accounts, client, client, keys, log, h.clock)

	h.authority, err = authz.New(bytes.Repeat([]byte("k"), authz.MinKeyLength))
	if err != nil {
		t.Fatalf("authority: %v", err)
	}
	srv := grpc.NewServer(
		grpc.UnaryInterceptor(h.authority.UnaryServerInterceptor()),
		grpc.StreamInterceptor(h.authority.StreamServerInterceptor()),
	)
	tsubamev1.RegisterTsubameServiceServer(srv, tsubamegrpc.New(connector))
	lis := bufconn.Listen(1 << 20)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	h.client = tsubamev1.NewTsubameServiceClient(conn)
	return h
}

type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// ctxFor returns a context whose calls carry an identity token for owner.
func (h *harness) ctxFor(t *testing.T, owner string) context.Context {
	t.Helper()
	token, err := h.authority.Sign(authz.Identity{OwnerID: owner, RequestID: "test-request"})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return metadata.AppendToOutgoingContext(context.Background(), authz.Header, token)
}

func (h *harness) ctx(t *testing.T) context.Context { return h.ctxFor(t, h.owner) }

// begin starts a connection and returns the state the provider would send back.
func (h *harness) begin(ctx context.Context, t *testing.T) (authURL, state string) {
	t.Helper()
	res, err := h.client.ConnectAccount(ctx, &tsubamev1.ConnectAccountRequest{Provider: tsubamev1.MailProvider_MAIL_PROVIDER_GMAIL})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	u, err := url.Parse(res.GetAuthUrl())
	if err != nil {
		t.Fatalf("auth url: %v", err)
	}
	return res.GetAuthUrl(), u.Query().Get("state")
}

// requireStatus fails unless err has the gRPC code and ErrorInfo reason.
func requireStatus(t *testing.T, err error, code codes.Code, reason string) {
	t.Helper()
	st, ok := status.FromError(err)
	if !ok || st.Code() != code {
		t.Fatalf("got %v, want code %s", err, code)
	}
	if reason == "" {
		return
	}
	for _, d := range st.Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok && info.GetReason() == reason {
			return
		}
	}
	t.Fatalf("got %v, want reason %s", err, reason)
}

func contains(s string, parts ...string) bool {
	for _, p := range parts {
		if p != "" && strings.Contains(s, p) {
			return true
		}
	}
	return false
}
