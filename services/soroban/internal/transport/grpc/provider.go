package grpc

import (
	"context"
	"errors"

	"google.golang.org/protobuf/types/known/timestamppb"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/services/soroban/internal/app"
	"github.com/0xHoaxen/shogun/services/soroban/internal/store"
)

// SetProvider implements soroban.v1.SorobanService.
func (s *Server) SetProvider(ctx context.Context, req *sorobanv1.SetProviderRequest) (*sorobanv1.SetProviderResponse, error) {
	view, err := s.svc.SetProvider(ctx, app.ProviderInput{
		BaseURL:             req.GetBaseUrl(),
		Model:               req.GetModel(),
		APIKey:              req.GetApiKey(),
		InputMicrosPerMTok:  req.InputMicrosPerMtok,
		OutputMicrosPerMTok: req.OutputMicrosPerMtok,
		MonthlyTokenCap:     req.MonthlyTokenCap,
		Version:             req.GetVersion(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	return &sorobanv1.SetProviderResponse{Provider: toProvider(view)}, nil
}

// GetProvider implements soroban.v1.SorobanService. A caller with no provider
// gets an empty answer, not an error.
func (s *Server) GetProvider(ctx context.Context, _ *sorobanv1.GetProviderRequest) (*sorobanv1.GetProviderResponse, error) {
	view, err := s.svc.GetProvider(ctx)
	if errors.Is(err, store.ErrNotFound) {
		return &sorobanv1.GetProviderResponse{}, nil
	}
	if err != nil {
		return nil, toStatus(err)
	}
	tokens, err := s.svc.TokensThisMonth(ctx)
	if err != nil {
		return nil, toStatus(err)
	}
	return &sorobanv1.GetProviderResponse{Provider: toProvider(view), TokensThisMonth: tokens}, nil
}

// DeleteProvider implements soroban.v1.SorobanService.
func (s *Server) DeleteProvider(ctx context.Context, _ *sorobanv1.DeleteProviderRequest) (*sorobanv1.DeleteProviderResponse, error) {
	if err := s.svc.DeleteProvider(ctx); err != nil {
		return nil, toStatus(err)
	}
	return &sorobanv1.DeleteProviderResponse{}, nil
}

// toProvider converts a stored provider to its wire form, which has no key.
func toProvider(v app.ProviderView) *sorobanv1.Provider {
	return &sorobanv1.Provider{
		BaseUrl:             v.BaseURL,
		Model:               v.Model,
		KeyLast4:            v.KeyLast4,
		InputMicrosPerMtok:  v.InputMicrosPerMTok,
		OutputMicrosPerMtok: v.OutputMicrosPerMTok,
		MonthlyTokenCap:     v.MonthlyTokenCap,
		Version:             v.Version,
		UpdatedAt:           timestamppb.New(v.UpdatedAt),
	}
}
