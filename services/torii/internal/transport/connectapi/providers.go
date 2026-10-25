package connectapi

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"connectrpc.com/connect"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
)

// ProviderStore is the part of soroban's client ProviderServer uses: it keeps
// each user's provider with its key sealed.
type ProviderStore interface {
	SetProvider(ctx context.Context, in *sorobanv1.SetProviderRequest, opts ...grpc.CallOption) (*sorobanv1.SetProviderResponse, error)
	GetProvider(ctx context.Context, in *sorobanv1.GetProviderRequest, opts ...grpc.CallOption) (*sorobanv1.GetProviderResponse, error)
	DeleteProvider(ctx context.Context, in *sorobanv1.DeleteProviderRequest, opts ...grpc.CallOption) (*sorobanv1.DeleteProviderResponse, error)
}

// ProviderTester is the part of fude's client ProviderServer uses to try a
// provider out.
type ProviderTester interface {
	TestProvider(ctx context.Context, in *fudev1.TestProviderRequest, opts ...grpc.CallOption) (*fudev1.TestProviderResponse, error)
}

// ProviderServer implements shogun.api.v1.ProviderService. It never logs a
// request, since SetProvider carries an API key, and the key is never returned.
type ProviderServer struct {
	store  ProviderStore
	tester ProviderTester
	log    *slog.Logger
}

var _ apiv1connect.ProviderServiceHandler = (*ProviderServer)(nil)

// NewProviderServer returns a ProviderServer that keeps providers in store and
// tests them through tester.
func NewProviderServer(store ProviderStore, tester ProviderTester, log *slog.Logger) *ProviderServer {
	return &ProviderServer{store: store, tester: tester, log: log}
}

// GetProvider returns the caller's provider without its key.
func (s *ProviderServer) GetProvider(
	ctx context.Context, _ *connect.Request[apiv1.GetProviderRequest],
) (*connect.Response[apiv1.GetProviderResponse], error) {
	resp, err := s.store.GetProvider(ctx, &sorobanv1.GetProviderRequest{})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.GetProviderResponse{
		Provider:        providerToAPI(resp.GetProvider()),
		TokensThisMonth: resp.GetTokensThisMonth(),
	}), nil
}

// SetProvider stores the caller's provider. The key goes straight to soroban,
// which seals it; it is not kept, logged or returned here.
func (s *ProviderServer) SetProvider(
	ctx context.Context, req *connect.Request[apiv1.SetProviderRequest],
) (*connect.Response[apiv1.SetProviderResponse], error) {
	in := req.Msg
	// A provider that does not work is never saved.
	if err := s.checkBeforeSave(ctx, in); err != nil {
		return nil, err
	}
	resp, err := s.store.SetProvider(ctx, &sorobanv1.SetProviderRequest{
		BaseUrl:             in.GetBaseUrl(),
		Model:               in.GetModel(),
		ApiKey:              in.GetApiKey(),
		InputMicrosPerMtok:  in.InputMicrosPerMtok,
		OutputMicrosPerMtok: in.OutputMicrosPerMtok,
		MonthlyTokenCap:     in.MonthlyTokenCap,
		Version:             in.GetVersion(),
	})
	if err != nil {
		// The key is the one secret that passes through here, so it is scrubbed
		// from whatever comes back before it is logged or returned.
		return nil, fromGRPC(ctx, s.log, redactSecret(err, in.GetApiKey()))
	}
	return connect.NewResponse(&apiv1.SetProviderResponse{Provider: providerToAPI(resp.GetProvider())}), nil
}

// DeleteProvider removes the caller's provider and key.
func (s *ProviderServer) DeleteProvider(
	ctx context.Context, _ *connect.Request[apiv1.DeleteProviderRequest],
) (*connect.Response[apiv1.DeleteProviderResponse], error) {
	if _, err := s.store.DeleteProvider(ctx, &sorobanv1.DeleteProviderRequest{}); err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.DeleteProviderResponse{}), nil
}

// TestProvider tries the caller's provider with one small call.
func (s *ProviderServer) TestProvider(
	ctx context.Context, req *connect.Request[apiv1.TestProviderRequest],
) (*connect.Response[apiv1.TestProviderResponse], error) {
	in := req.Msg
	resp, err := s.tester.TestProvider(ctx, &fudev1.TestProviderRequest{
		BaseUrl: in.BaseUrl, Model: in.Model, ApiKey: in.ApiKey,
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, redactSecret(err, in.GetApiKey()))
	}
	return connect.NewResponse(&apiv1.TestProviderResponse{
		Ok:        resp.GetOk(),
		Failure:   providerFailureToAPI(resp.GetFailure()),
		Model:     resp.GetModel(),
		LatencyMs: resp.GetLatencyMs(),
	}), nil
}

// checkBeforeSave makes sure a provider that does not work is never saved. With
// a key it is tested with the details as they will be stored. Without one (the
// stored key is kept) the stored address and model must be unchanged: the key is
// not available here to test a new one with. An edit of prices or the cap only
// makes no call.
func (s *ProviderServer) checkBeforeSave(ctx context.Context, in *apiv1.SetProviderRequest) error {
	if in.GetApiKey() == "" {
		return s.checkUnchanged(ctx, in)
	}
	resp, err := s.tester.TestProvider(ctx, &fudev1.TestProviderRequest{
		BaseUrl: &in.BaseUrl, Model: &in.Model, ApiKey: &in.ApiKey,
	})
	if err != nil {
		return fromGRPC(ctx, s.log, redactSecret(err, in.GetApiKey()))
	}
	if resp.GetOk() {
		return nil
	}
	return testFailedError(providerFailureToAPI(resp.GetFailure()))
}

// checkUnchanged refuses an update that keeps the stored key but changes the
// address or model, which could not be tested.
func (s *ProviderServer) checkUnchanged(ctx context.Context, in *apiv1.SetProviderRequest) error {
	if in.GetVersion() == 0 {
		return nil // creating: soroban refuses a missing key with API_KEY_REQUIRED
	}
	current, err := s.store.GetProvider(ctx, &sorobanv1.GetProviderRequest{})
	if err != nil {
		return fromGRPC(ctx, s.log, err)
	}
	stored := current.GetProvider()
	if stored == nil || (sameAddress(stored.GetBaseUrl(), in.GetBaseUrl()) && strings.TrimSpace(in.GetModel()) == stored.GetModel()) {
		return nil
	}
	return newError(connect.CodeInvalidArgument, reasonAPIKeyRequired,
		"enter the API key again to change the address or the model, so the new details can be tested")
}

// sameAddress compares two provider addresses the way soroban stores them: no
// surrounding space and no trailing slash.
func sameAddress(a, b string) bool {
	return strings.TrimRight(strings.TrimSpace(a), "/") == strings.TrimRight(strings.TrimSpace(b), "/")
}

// testFailedError is the refusal of a provider whose test failed. The kind of
// failure is in the metadata, so the client can say what to fix.
func testFailedError(failure apiv1.ProviderFailure) error {
	err := connect.NewError(connect.CodeFailedPrecondition, errors.New("the provider did not pass its test, so it was not saved"))
	detail, detailErr := connect.NewErrorDetail(&errdetails.ErrorInfo{
		Reason: reasonProviderTestFailed, Domain: errorDomain, Metadata: map[string]string{"failure": failure.String()},
	})
	if detailErr == nil {
		err.AddDetail(detail)
	}
	return err
}

// redactSecret hides secret in err's message. A gRPC status keeps its code and
// details; any other error becomes a plain error with the same text, scrubbed.
func redactSecret(err error, secret string) error {
	if err == nil || secret == "" {
		return err
	}
	if st, ok := status.FromError(err); ok {
		if !strings.Contains(st.Message(), secret) {
			return err
		}
		p := st.Proto()
		p.Message = strings.ReplaceAll(p.Message, secret, "[redacted]")
		return status.FromProto(p).Err()
	}
	if !strings.Contains(err.Error(), secret) {
		return err
	}
	return errors.New(strings.ReplaceAll(err.Error(), secret, "[redacted]"))
}

// providerToAPI converts a stored provider, or nil when there is none.
func providerToAPI(p *sorobanv1.Provider) *apiv1.Provider {
	if p == nil {
		return nil
	}
	return &apiv1.Provider{
		BaseUrl:             p.GetBaseUrl(),
		Model:               p.GetModel(),
		KeyLast4:            p.GetKeyLast4(),
		InputMicrosPerMtok:  p.InputMicrosPerMtok,
		OutputMicrosPerMtok: p.OutputMicrosPerMtok,
		MonthlyTokenCap:     p.MonthlyTokenCap,
		Version:             p.GetVersion(),
		UpdatedAt:           timestamppb.New(p.GetUpdatedAt().AsTime()),
	}
}

// providerFailureToAPI maps fude's failure enum to the API's. One added later
// and not mapped yet shows as other.
func providerFailureToAPI(f fudev1.ProviderFailure) apiv1.ProviderFailure {
	switch f {
	case fudev1.ProviderFailure_PROVIDER_FAILURE_UNSPECIFIED:
		return apiv1.ProviderFailure_PROVIDER_FAILURE_UNSPECIFIED
	case fudev1.ProviderFailure_PROVIDER_FAILURE_REJECTED:
		return apiv1.ProviderFailure_PROVIDER_FAILURE_REJECTED
	case fudev1.ProviderFailure_PROVIDER_FAILURE_RATE_LIMITED:
		return apiv1.ProviderFailure_PROVIDER_FAILURE_RATE_LIMITED
	case fudev1.ProviderFailure_PROVIDER_FAILURE_NOT_FOUND:
		return apiv1.ProviderFailure_PROVIDER_FAILURE_NOT_FOUND
	case fudev1.ProviderFailure_PROVIDER_FAILURE_UNREACHABLE:
		return apiv1.ProviderFailure_PROVIDER_FAILURE_UNREACHABLE
	case fudev1.ProviderFailure_PROVIDER_FAILURE_NOT_PUBLIC:
		return apiv1.ProviderFailure_PROVIDER_FAILURE_NOT_PUBLIC
	case fudev1.ProviderFailure_PROVIDER_FAILURE_PROVIDER_ERROR:
		return apiv1.ProviderFailure_PROVIDER_FAILURE_PROVIDER_ERROR
	case fudev1.ProviderFailure_PROVIDER_FAILURE_ENDPOINT_IN_URL:
		return apiv1.ProviderFailure_PROVIDER_FAILURE_ENDPOINT_IN_URL
	case fudev1.ProviderFailure_PROVIDER_FAILURE_INVALID_ADDRESS:
		return apiv1.ProviderFailure_PROVIDER_FAILURE_INVALID_ADDRESS
	default:
		return apiv1.ProviderFailure_PROVIDER_FAILURE_OTHER
	}
}
