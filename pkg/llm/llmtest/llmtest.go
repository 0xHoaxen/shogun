// Package llmtest has fakes of the model and of soroban for tests of code that
// calls llm.Client. They record their calls and are not safe to inspect while
// a call is running.
package llmtest

import (
	"context"
	"sync"

	"google.golang.org/grpc"

	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/pkg/llm"
)

// API is a fake llm.API.
type API struct {
	mu sync.Mutex

	// Tokens is what CountTokens returns.
	Tokens int32
	// Result is what Create returns.
	Result llm.CallResult
	// CountErr and CreateErr make the matching call fail.
	CountErr, CreateErr error
	// OnCreate, if set, runs at the start of Create.
	OnCreate func(ctx context.Context)

	// Counted and Created are the requests received.
	Counted, Created []llm.CallRequest
}

// CountTokens implements llm.API.
func (a *API) CountTokens(_ context.Context, req llm.CallRequest) (int32, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Counted = append(a.Counted, req)
	return a.Tokens, a.CountErr
}

// Create implements llm.API.
func (a *API) Create(ctx context.Context, req llm.CallRequest) (llm.CallResult, error) {
	if a.OnCreate != nil {
		a.OnCreate(ctx)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Created = append(a.Created, req)
	return a.Result, a.CreateErr
}

// Meter is a fake llm.Meter.
type Meter struct {
	mu sync.Mutex

	// ReservationID is what Reserve returns; empty means "reservation-1".
	ReservationID string
	// ReserveErr makes Reserve fail.
	ReserveErr error
	// FailCommits makes the first n Commit calls fail.
	FailCommits int
	// ReleaseErr makes Release fail.
	ReleaseErr error

	// Reserved, Committed and Released are the requests received.
	Reserved  []*sorobanv1.ReserveRequest
	Committed []*sorobanv1.CommitRequest
	Released  []*sorobanv1.ReleaseRequest
	// CommitCtxErrs is the context error each Commit saw; nil means the
	// context was still live.
	CommitCtxErrs []error
}

// Reserve implements llm.Meter.
func (m *Meter) Reserve(_ context.Context, in *sorobanv1.ReserveRequest, _ ...grpc.CallOption) (*sorobanv1.ReserveResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Reserved = append(m.Reserved, in)
	if m.ReserveErr != nil {
		return nil, m.ReserveErr
	}
	id := m.ReservationID
	if id == "" {
		id = "reservation-1"
	}
	return &sorobanv1.ReserveResponse{ReservationId: id}, nil
}

// Commit implements llm.Meter.
func (m *Meter) Commit(ctx context.Context, in *sorobanv1.CommitRequest, _ ...grpc.CallOption) (*sorobanv1.CommitResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Committed = append(m.Committed, in)
	m.CommitCtxErrs = append(m.CommitCtxErrs, ctx.Err())
	if len(m.Committed) <= m.FailCommits {
		return nil, context.DeadlineExceeded
	}
	return &sorobanv1.CommitResponse{}, nil
}

// Release implements llm.Meter.
func (m *Meter) Release(_ context.Context, in *sorobanv1.ReleaseRequest, _ ...grpc.CallOption) (*sorobanv1.ReleaseResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Released = append(m.Released, in)
	return &sorobanv1.ReleaseResponse{}, m.ReleaseErr
}

var (
	_ llm.API   = (*API)(nil)
	_ llm.Meter = (*Meter)(nil)
)
