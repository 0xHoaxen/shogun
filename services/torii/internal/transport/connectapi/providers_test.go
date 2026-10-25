package connectapi_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/app/apptest"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/connectapi"
)

const apiKey = "sk-live-0123456789-SECRET"

// fakeProviders answers the provider calls of soroban and fude from funcs a test
// sets, and records the owner each acted for and the requests it saw.
type fakeProviders struct {
	set    func(*sorobanv1.SetProviderRequest) (*sorobanv1.SetProviderResponse, error)
	get    func() (*sorobanv1.GetProviderResponse, error)
	del    func() error
	test   func() (*fudev1.TestProviderResponse, error)
	mu     sync.Mutex
	owners []string
	reqs   []any
}

func (f *fakeProviders) record(ctx context.Context, in any) {
	id, _ := authz.FromContext(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owners = append(f.owners, id.OwnerID)
	f.reqs = append(f.reqs, in)
}

func (f *fakeProviders) SetProvider(ctx context.Context, in *sorobanv1.SetProviderRequest, _ ...grpc.CallOption) (*sorobanv1.SetProviderResponse, error) {
	f.record(ctx, in)
	return f.set(in)
}

func (f *fakeProviders) GetProvider(ctx context.Context, in *sorobanv1.GetProviderRequest, _ ...grpc.CallOption) (*sorobanv1.GetProviderResponse, error) {
	f.record(ctx, in)
	return f.get()
}

func (f *fakeProviders) DeleteProvider(ctx context.Context, in *sorobanv1.DeleteProviderRequest, _ ...grpc.CallOption) (*sorobanv1.DeleteProviderResponse, error) {
	f.record(ctx, in)
	return &sorobanv1.DeleteProviderResponse{}, f.del()
}

func (f *fakeProviders) TestProvider(ctx context.Context, in *fudev1.TestProviderRequest, _ ...grpc.CallOption) (*fudev1.TestProviderResponse, error) {
	f.record(ctx, in)
	return f.test()
}

type providersHarness struct {
	fake   *fakeProviders
	client apiv1connect.ProviderServiceClient
	token  string
	owner  string
	logs   *bytes.Buffer
}

func newProvidersHarness(t *testing.T) *providersHarness {
	t.Helper()
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	auth, err := app.NewAuth(apptest.NewMemSessions(), apptest.NewMemUsers(),
		app.AuthConfig{AdminEmails: []string{ownerEmail}, SessionTTL: sessionTTL},
		app.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	token, session, err := auth.StartSession(context.Background(), app.Claims{Subject: "sub-1", Email: ownerEmail, EmailVerified: true})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	logs := &bytes.Buffer{}
	log := slog.New(slog.NewJSONHandler(logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	// A provider passes its test unless a test says otherwise.
	fake := &fakeProviders{test: func() (*fudev1.TestProviderResponse, error) {
		return &fudev1.TestProviderResponse{Ok: true, Model: "my-llama"}, nil
	}}
	mux := http.NewServeMux()
	mux.Handle(apiv1connect.NewProviderServiceHandler(connectapi.NewProviderServer(fake, fake, log),
		connect.WithInterceptors(connectapi.NewSessionInterceptor(connectapi.InterceptorConfig{Auth: auth, Log: log}))))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &providersHarness{
		fake: fake, token: token, owner: session.OwnerID.String(), logs: logs,
		client: apiv1connect.NewProviderServiceClient(srv.Client(), srv.URL),
	}
}

func storedProvider() *sorobanv1.Provider {
	in, out, limit := int64(3_000_000), int64(15_000_000), int64(500_000)
	return &sorobanv1.Provider{
		BaseUrl: "https://llm.example.com", Model: "my-llama", KeyLast4: "CRET",
		InputMicrosPerMtok: &in, OutputMicrosPerMtok: &out, MonthlyTokenCap: &limit,
		Version: 3, UpdatedAt: timestamppb.New(time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)),
	}
}

// sorobanSet returns the one SetProviderRequest soroban received.
func (f *fakeProviders) sorobanSet(t *testing.T) *sorobanv1.SetProviderRequest {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	var found *sorobanv1.SetProviderRequest
	for _, r := range f.reqs {
		if set, ok := r.(*sorobanv1.SetProviderRequest); ok {
			if found != nil {
				t.Fatal("soroban got more than one SetProvider")
			}
			found = set
		}
	}
	if found == nil {
		t.Fatal("soroban got no SetProvider")
	}
	return found
}

// calls counts the requests of the given type the fake saw.
func calls[T any](f *fakeProviders) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, r := range f.reqs {
		if _, ok := r.(T); ok {
			n++
		}
	}
	return n
}

func rpcError(code codes.Code, reason, msg string) error {
	st, err := status.New(code, msg).WithDetails(&errdetails.ErrorInfo{Reason: reason, Domain: "soroban.shogun"})
	if err != nil {
		panic(err)
	}
	return st.Err()
}

func TestSetProviderPassesEverythingToSorobanAsTheOwnerAndReturnsNoKey(t *testing.T) {
	// Arrange
	h := newProvidersHarness(t)
	h.fake.set = func(*sorobanv1.SetProviderRequest) (*sorobanv1.SetProviderResponse, error) {
		return &sorobanv1.SetProviderResponse{Provider: storedProvider()}, nil
	}
	in, out, limit := int64(3_000_000), int64(15_000_000), int64(500_000)

	// Act
	resp, err := h.client.SetProvider(context.Background(), withCookie(connect.NewRequest(&apiv1.SetProviderRequest{
		BaseUrl: "https://llm.example.com", Model: "my-llama", ApiKey: apiKey, Version: 2,
		InputMicrosPerMtok: &in, OutputMicrosPerMtok: &out, MonthlyTokenCap: &limit,
	}), h.token))
	// Assert
	if err != nil {
		t.Fatalf("SetProvider: %v", err)
	}
	got := h.fake.sorobanSet(t)
	if got.GetBaseUrl() != "https://llm.example.com" || got.GetModel() != "my-llama" || got.GetApiKey() != apiKey || got.GetVersion() != 2 ||
		got.GetInputMicrosPerMtok() != in || got.GetOutputMicrosPerMtok() != out || got.GetMonthlyTokenCap() != limit {
		t.Fatalf("soroban got %v", got)
	}
	if h.fake.owners[0] != h.owner {
		t.Fatalf("soroban acted for %q, want the signed-in owner %q", h.fake.owners[0], h.owner)
	}
	p := resp.Msg.GetProvider()
	if p.GetKeyLast4() != "CRET" || p.GetVersion() != 3 || p.GetMonthlyTokenCap() != limit || p.GetInputMicrosPerMtok() != in {
		t.Fatalf("provider = %v", p)
	}
	if strings.Contains(resp.Msg.String(), apiKey) {
		t.Fatalf("the response carries the key: %v", resp.Msg)
	}
}

func TestTheKeyNeverReachesALogEvenWhenTheCallFails(t *testing.T) {
	h := newProvidersHarness(t)
	h.fake.set = func(*sorobanv1.SetProviderRequest) (*sorobanv1.SetProviderResponse, error) {
		return nil, status.Error(codes.Internal, "boom with "+apiKey) // a downstream that leaks it
	}

	_, err := h.client.SetProvider(context.Background(), withCookie(connect.NewRequest(&apiv1.SetProviderRequest{
		BaseUrl: "https://llm.example.com", Model: "m", ApiKey: apiKey,
	}), h.token))

	if err == nil {
		t.Fatal("want the failure")
	}
	if strings.Contains(h.logs.String(), apiKey) || strings.Contains(err.Error(), apiKey) {
		t.Fatalf("the key leaked.\nlogs: %s\nerror: %v", h.logs.String(), err)
	}
}

func TestSetProviderKeepsTheReasonsTheUserCanActOn(t *testing.T) {
	for _, tt := range []struct {
		code   codes.Code
		reason string
	}{
		{codes.InvalidArgument, "INVALID_PROVIDER_URL"},
		{codes.InvalidArgument, "INVALID_PROVIDER_MODEL"},
		{codes.InvalidArgument, "INVALID_API_KEY"},
		{codes.InvalidArgument, "API_KEY_REQUIRED"},
		{codes.InvalidArgument, "INVALID_PRICE"},
		{codes.InvalidArgument, "INVALID_TOKEN_CAP"},
		{codes.Aborted, "VERSION_CONFLICT"},
	} {
		t.Run(tt.reason, func(t *testing.T) {
			h := newProvidersHarness(t)
			h.fake.set = func(*sorobanv1.SetProviderRequest) (*sorobanv1.SetProviderResponse, error) {
				return nil, rpcError(tt.code, tt.reason, "refused")
			}

			_, err := h.client.SetProvider(context.Background(), withCookie(connect.NewRequest(&apiv1.SetProviderRequest{Model: "m"}), h.token))

			if connect.CodeOf(err) != connect.Code(tt.code) || reasonOf(err) != tt.reason {
				t.Fatalf("code %v reason %q, want %v %s", connect.CodeOf(err), reasonOf(err), connect.Code(tt.code), tt.reason)
			}
		})
	}
}

func TestGetProviderMapsTheStoredProviderAndTheMonthsTokens(t *testing.T) {
	h := newProvidersHarness(t)
	h.fake.get = func() (*sorobanv1.GetProviderResponse, error) {
		return &sorobanv1.GetProviderResponse{Provider: storedProvider(), TokensThisMonth: 12345}, nil
	}

	resp, err := h.client.GetProvider(context.Background(), withCookie(connect.NewRequest(&apiv1.GetProviderRequest{}), h.token))

	if err != nil || resp.Msg.GetTokensThisMonth() != 12345 || resp.Msg.GetProvider().GetModel() != "my-llama" ||
		resp.Msg.GetProvider().GetUpdatedAt() == nil {
		t.Fatalf("resp %v, err %v", resp, err)
	}
}

func TestGetProviderForAUserWithNoneReturnsNone(t *testing.T) {
	h := newProvidersHarness(t)
	h.fake.get = func() (*sorobanv1.GetProviderResponse, error) { return &sorobanv1.GetProviderResponse{}, nil }

	resp, err := h.client.GetProvider(context.Background(), withCookie(connect.NewRequest(&apiv1.GetProviderRequest{}), h.token))

	if err != nil || resp.Msg.GetProvider() != nil {
		t.Fatalf("resp %v, err %v; want no provider", resp, err)
	}
}

func TestDeleteProviderRemovesItForTheOwner(t *testing.T) {
	h := newProvidersHarness(t)
	h.fake.del = func() error { return nil }

	_, err := h.client.DeleteProvider(context.Background(), withCookie(connect.NewRequest(&apiv1.DeleteProviderRequest{}), h.token))

	if err != nil || len(h.fake.reqs) != 1 || h.fake.owners[0] != h.owner {
		t.Fatalf("err %v, calls %d, owners %v", err, len(h.fake.reqs), h.fake.owners)
	}
}

func TestTestProviderReportsAWorkingAndAFailingProvider(t *testing.T) {
	h := newProvidersHarness(t)
	h.fake.test = func() (*fudev1.TestProviderResponse, error) {
		return &fudev1.TestProviderResponse{Ok: true, Model: "my-llama"}, nil
	}
	working, err := h.client.TestProvider(context.Background(), withCookie(connect.NewRequest(&apiv1.TestProviderRequest{}), h.token))
	if err != nil || !working.Msg.GetOk() || working.Msg.GetModel() != "my-llama" {
		t.Fatalf("working: %v, %v", working, err)
	}

	for from, want := range map[fudev1.ProviderFailure]apiv1.ProviderFailure{
		fudev1.ProviderFailure_PROVIDER_FAILURE_REJECTED:       apiv1.ProviderFailure_PROVIDER_FAILURE_REJECTED,
		fudev1.ProviderFailure_PROVIDER_FAILURE_RATE_LIMITED:   apiv1.ProviderFailure_PROVIDER_FAILURE_RATE_LIMITED,
		fudev1.ProviderFailure_PROVIDER_FAILURE_NOT_FOUND:      apiv1.ProviderFailure_PROVIDER_FAILURE_NOT_FOUND,
		fudev1.ProviderFailure_PROVIDER_FAILURE_UNREACHABLE:    apiv1.ProviderFailure_PROVIDER_FAILURE_UNREACHABLE,
		fudev1.ProviderFailure_PROVIDER_FAILURE_NOT_PUBLIC:     apiv1.ProviderFailure_PROVIDER_FAILURE_NOT_PUBLIC,
		fudev1.ProviderFailure_PROVIDER_FAILURE_PROVIDER_ERROR: apiv1.ProviderFailure_PROVIDER_FAILURE_PROVIDER_ERROR,
		fudev1.ProviderFailure_PROVIDER_FAILURE_OTHER:          apiv1.ProviderFailure_PROVIDER_FAILURE_OTHER,
	} {
		h.fake.test = func() (*fudev1.TestProviderResponse, error) { return &fudev1.TestProviderResponse{Failure: from}, nil }

		resp, err := h.client.TestProvider(context.Background(), withCookie(connect.NewRequest(&apiv1.TestProviderRequest{}), h.token))

		if err != nil || resp.Msg.GetOk() || resp.Msg.GetFailure() != want {
			t.Errorf("%v: resp %v, err %v; want ok=false and %v", from, resp, err, want)
		}
	}
}

func TestTestProviderKeepsWhatTheUserMustFixOrWaitOut(t *testing.T) {
	for _, tt := range []struct {
		code   codes.Code
		reason string
	}{
		{codes.FailedPrecondition, "AI_PROVIDER_NOT_CONFIGURED"},
		{codes.ResourceExhausted, "BUDGET_EXHAUSTED"},
	} {
		t.Run(tt.reason, func(t *testing.T) {
			h := newProvidersHarness(t)
			h.fake.test = func() (*fudev1.TestProviderResponse, error) { return nil, rpcError(tt.code, tt.reason, "refused") }

			_, err := h.client.TestProvider(context.Background(), withCookie(connect.NewRequest(&apiv1.TestProviderRequest{}), h.token))

			if connect.CodeOf(err) != connect.Code(tt.code) || reasonOf(err) != tt.reason {
				t.Fatalf("code %v reason %q, want %v %s", connect.CodeOf(err), reasonOf(err), connect.Code(tt.code), tt.reason)
			}
		})
	}
}

func TestProviderCallsNeedASessionAndNeverTouchDownstreamWithout(t *testing.T) {
	h := newProvidersHarness(t)

	_, get := h.client.GetProvider(context.Background(), connect.NewRequest(&apiv1.GetProviderRequest{}))
	_, set := h.client.SetProvider(context.Background(), connect.NewRequest(&apiv1.SetProviderRequest{ApiKey: apiKey}))
	_, del := h.client.DeleteProvider(context.Background(), connect.NewRequest(&apiv1.DeleteProviderRequest{}))
	_, test := h.client.TestProvider(context.Background(), connect.NewRequest(&apiv1.TestProviderRequest{}))

	for name, err := range map[string]error{"get": get, "set": set, "delete": del, "test": test} {
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("%s: code %v, want Unauthenticated", name, connect.CodeOf(err))
		}
	}
	if len(h.fake.reqs) != 0 {
		t.Fatalf("%d downstream calls were made without a session", len(h.fake.reqs))
	}
}

func str(v string) *string { return &v }

func setReq(h *providersHarness, in *apiv1.SetProviderRequest) (*connect.Response[apiv1.SetProviderResponse], error) {
	return h.client.SetProvider(context.Background(), withCookie(connect.NewRequest(in), h.token))
}

func (h *providersHarness) savesOK() {
	h.fake.set = func(*sorobanv1.SetProviderRequest) (*sorobanv1.SetProviderResponse, error) {
		return &sorobanv1.SetProviderResponse{Provider: storedProvider()}, nil
	}
}

func TestTestProviderPassesTypedDetailsToFudeAndReportsLatency(t *testing.T) {
	h := newProvidersHarness(t)
	h.fake.test = func() (*fudev1.TestProviderResponse, error) {
		return &fudev1.TestProviderResponse{Ok: true, Model: "my-llama", LatencyMs: 420}, nil
	}

	resp, err := h.client.TestProvider(context.Background(), withCookie(connect.NewRequest(&apiv1.TestProviderRequest{
		BaseUrl: str("https://llm.example.com/v1"), Model: str("my-llama"), ApiKey: str(apiKey),
	}), h.token))

	if err != nil || !resp.Msg.GetOk() || resp.Msg.GetLatencyMs() != 420 {
		t.Fatalf("resp %v, err %v", resp, err)
	}
	got := h.fake.reqs[0].(*fudev1.TestProviderRequest)
	if got.GetBaseUrl() != "https://llm.example.com/v1" || got.GetModel() != "my-llama" || got.GetApiKey() != apiKey {
		t.Fatalf("fude got %v, want the typed details", got)
	}
	if strings.Contains(resp.Msg.String(), apiKey) || strings.Contains(h.logs.String(), apiKey) {
		t.Fatal("the key leaked into the response or the logs")
	}
}

func TestTestProviderMapsTheNewFailureKinds(t *testing.T) {
	for from, want := range map[fudev1.ProviderFailure]apiv1.ProviderFailure{
		fudev1.ProviderFailure_PROVIDER_FAILURE_ENDPOINT_IN_URL: apiv1.ProviderFailure_PROVIDER_FAILURE_ENDPOINT_IN_URL,
		fudev1.ProviderFailure_PROVIDER_FAILURE_INVALID_ADDRESS: apiv1.ProviderFailure_PROVIDER_FAILURE_INVALID_ADDRESS,
	} {
		h := newProvidersHarness(t)
		h.fake.test = func() (*fudev1.TestProviderResponse, error) { return &fudev1.TestProviderResponse{Failure: from}, nil }

		resp, err := h.client.TestProvider(context.Background(), withCookie(connect.NewRequest(&apiv1.TestProviderRequest{}), h.token))

		if err != nil || resp.Msg.GetOk() || resp.Msg.GetFailure() != want {
			t.Fatalf("%v: resp %v, err %v; want %v", from, resp, err, want)
		}
	}
}

func TestTestProviderScrubsTheTypedKeyFromAnError(t *testing.T) {
	h := newProvidersHarness(t)
	h.fake.test = func() (*fudev1.TestProviderResponse, error) {
		return nil, rpcError(codes.InvalidArgument, "INVALID_REQUEST", "bad "+apiKey)
	}

	_, err := h.client.TestProvider(context.Background(), withCookie(connect.NewRequest(&apiv1.TestProviderRequest{
		BaseUrl: str("https://a.example.com"), Model: str("m"), ApiKey: str(apiKey),
	}), h.token))

	if err == nil || strings.Contains(err.Error(), apiKey) || strings.Contains(h.logs.String(), apiKey) {
		t.Fatalf("err %v; want a refusal that does not repeat the key", err)
	}
}

func TestSetProviderWithAKeyTestsTheDetailsFirstAndThenSaves(t *testing.T) {
	h := newProvidersHarness(t)
	h.savesOK()

	_, err := setReq(h, &apiv1.SetProviderRequest{BaseUrl: "https://llm.example.com", Model: "my-llama", ApiKey: apiKey})
	if err != nil {
		t.Fatalf("SetProvider: %v", err)
	}
	if calls[*fudev1.TestProviderRequest](h.fake) != 1 || calls[*sorobanv1.SetProviderRequest](h.fake) != 1 {
		t.Fatalf("calls: %d tests, %d saves; want one of each", calls[*fudev1.TestProviderRequest](h.fake), calls[*sorobanv1.SetProviderRequest](h.fake))
	}
	if _, ok := h.fake.reqs[0].(*fudev1.TestProviderRequest); !ok {
		t.Fatalf("the first call was %T, want the test before the save", h.fake.reqs[0])
	}
	test := h.fake.reqs[0].(*fudev1.TestProviderRequest)
	if test.GetBaseUrl() != "https://llm.example.com" || test.GetModel() != "my-llama" || test.GetApiKey() != apiKey {
		t.Fatalf("tested %v, want the details being saved", test)
	}
}

func TestSetProviderNeverSavesAProviderThatFailsItsTest(t *testing.T) {
	for _, failure := range []fudev1.ProviderFailure{
		fudev1.ProviderFailure_PROVIDER_FAILURE_REJECTED,
		fudev1.ProviderFailure_PROVIDER_FAILURE_NOT_FOUND,
		fudev1.ProviderFailure_PROVIDER_FAILURE_UNREACHABLE,
		fudev1.ProviderFailure_PROVIDER_FAILURE_NOT_PUBLIC,
		fudev1.ProviderFailure_PROVIDER_FAILURE_ENDPOINT_IN_URL,
		fudev1.ProviderFailure_PROVIDER_FAILURE_INVALID_ADDRESS,
		fudev1.ProviderFailure_PROVIDER_FAILURE_OTHER,
	} {
		t.Run(failure.String(), func(t *testing.T) {
			h := newProvidersHarness(t)
			h.savesOK()
			h.fake.test = func() (*fudev1.TestProviderResponse, error) {
				return &fudev1.TestProviderResponse{Failure: failure}, nil
			}

			_, err := setReq(h, &apiv1.SetProviderRequest{BaseUrl: "https://llm.example.com", Model: "m", ApiKey: apiKey})

			if connect.CodeOf(err) != connect.CodeFailedPrecondition || reasonOf(err) != "PROVIDER_TEST_FAILED" {
				t.Fatalf("code %v reason %q, want FailedPrecondition PROVIDER_TEST_FAILED", connect.CodeOf(err), reasonOf(err))
			}
			if calls[*sorobanv1.SetProviderRequest](h.fake) != 0 {
				t.Fatal("a provider that failed its test was saved")
			}
			var ce *connect.Error
			if !errors.As(err, &ce) || metadataOf(ce)["failure"] != apiv1.ProviderFailure(failure).String() {
				t.Fatalf("metadata %v, want the failure %v", metadataOf(ce), failure)
			}
			if strings.Contains(err.Error(), apiKey) || strings.Contains(h.logs.String(), apiKey) {
				t.Fatal("the key leaked")
			}
		})
	}
}

func TestSetProviderKeepsWhatTheUserMustFixOrWaitOutFromTheTest(t *testing.T) {
	for _, tt := range []struct {
		code   codes.Code
		reason string
	}{
		{codes.FailedPrecondition, "AI_PROVIDER_NOT_CONFIGURED"},
		{codes.ResourceExhausted, "BUDGET_EXHAUSTED"},
		{codes.Unavailable, "METERING_UNAVAILABLE"},
	} {
		t.Run(tt.reason, func(t *testing.T) {
			h := newProvidersHarness(t)
			h.savesOK()
			h.fake.test = func() (*fudev1.TestProviderResponse, error) { return nil, rpcError(tt.code, tt.reason, "no") }

			_, err := setReq(h, &apiv1.SetProviderRequest{BaseUrl: "https://llm.example.com", Model: "m", ApiKey: apiKey})

			if err == nil || calls[*sorobanv1.SetProviderRequest](h.fake) != 0 {
				t.Fatalf("err %v, saves %d; want a refusal and no save", err, calls[*sorobanv1.SetProviderRequest](h.fake))
			}
		})
	}
}

func TestEditingOnlyPricesOrTheCapKeepsTheKeyAndMakesNoTestCall(t *testing.T) {
	h := newProvidersHarness(t)
	h.savesOK()
	h.fake.get = func() (*sorobanv1.GetProviderResponse, error) {
		return &sorobanv1.GetProviderResponse{Provider: storedProvider()}, nil
	}
	limit := int64(9)

	// the address differs only by a trailing slash and a space, which soroban tidies away
	_, err := setReq(h, &apiv1.SetProviderRequest{BaseUrl: " https://llm.example.com/ ", Model: "my-llama", Version: 3, MonthlyTokenCap: &limit})

	if err != nil || calls[*fudev1.TestProviderRequest](h.fake) != 0 || calls[*sorobanv1.SetProviderRequest](h.fake) != 1 {
		t.Fatalf("err %v, tests %d, saves %d; want a save with no test call", err, calls[*fudev1.TestProviderRequest](h.fake), calls[*sorobanv1.SetProviderRequest](h.fake))
	}
}

func TestChangingTheAddressOrModelWithoutTheKeyAsksForTheKey(t *testing.T) {
	for name, in := range map[string]*apiv1.SetProviderRequest{
		"new address": {BaseUrl: "https://other.example.com", Model: "my-llama", Version: 3},
		"new model":   {BaseUrl: "https://llm.example.com", Model: "another", Version: 3},
	} {
		t.Run(name, func(t *testing.T) {
			h := newProvidersHarness(t)
			h.savesOK()
			h.fake.get = func() (*sorobanv1.GetProviderResponse, error) {
				return &sorobanv1.GetProviderResponse{Provider: storedProvider()}, nil
			}

			_, err := setReq(h, in)

			if connect.CodeOf(err) != connect.CodeInvalidArgument || reasonOf(err) != "API_KEY_REQUIRED" {
				t.Fatalf("code %v reason %q, want InvalidArgument API_KEY_REQUIRED", connect.CodeOf(err), reasonOf(err))
			}
			if calls[*sorobanv1.SetProviderRequest](h.fake) != 0 || calls[*fudev1.TestProviderRequest](h.fake) != 0 {
				t.Fatal("something was called for details that cannot be tested")
			}
		})
	}
}

func TestCreatingWithoutAKeyIsLeftToSorobanToRefuse(t *testing.T) {
	h := newProvidersHarness(t)
	h.fake.set = func(*sorobanv1.SetProviderRequest) (*sorobanv1.SetProviderResponse, error) {
		return nil, rpcError(codes.InvalidArgument, "API_KEY_REQUIRED", "an API key is required")
	}

	_, err := setReq(h, &apiv1.SetProviderRequest{BaseUrl: "https://llm.example.com", Model: "m"})

	if reasonOf(err) != "API_KEY_REQUIRED" || calls[*fudev1.TestProviderRequest](h.fake) != 0 {
		t.Fatalf("reason %q, tests %d; want soroban's refusal and no test", reasonOf(err), calls[*fudev1.TestProviderRequest](h.fake))
	}
}

// metadataOf returns the ErrorInfo metadata of a Connect error.
func metadataOf(err *connect.Error) map[string]string {
	if err == nil {
		return nil
	}
	for _, detail := range err.Details() {
		if msg, derr := detail.Value(); derr == nil {
			if info, ok := msg.(*errdetails.ErrorInfo); ok {
				return info.GetMetadata()
			}
		}
	}
	return nil
}
