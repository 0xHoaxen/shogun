// Package downstream reads from the other services what the daily digest
// summarises. Calls go out through the generated gRPC clients and run as the
// owner in the context.
package downstream

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"

	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
)

// Kagami is the part of kagami's client Sources uses.
type Kagami interface {
	ListDueFollowUps(ctx context.Context, in *kagamiv1.ListDueFollowUpsRequest, opts ...grpc.CallOption) (*kagamiv1.ListDueFollowUpsResponse, error)
}

// Fude is the part of fude's client Sources uses.
type Fude interface {
	ListQueue(ctx context.Context, in *fudev1.ListQueueRequest, opts ...grpc.CallOption) (*fudev1.ListQueueResponse, error)
}

// Soroban is the part of soroban's client Sources uses.
type Soroban interface {
	GetSpend(ctx context.Context, in *sorobanv1.GetSpendRequest, opts ...grpc.CallOption) (*sorobanv1.GetSpendResponse, error)
}

// Sources implements app.DigestSources on kagami, fude and soroban.
type Sources struct {
	kagami  Kagami
	fude    Fude
	soroban Soroban
}

// New returns Sources that call the given clients.
func New(kagami Kagami, fude Fude, soroban Soroban) *Sources {
	return &Sources{kagami: kagami, fude: fude, soroban: soroban}
}

// FollowUpsDue counts the jobs and contacts due on or before date.
func (s *Sources) FollowUpsDue(ctx context.Context, date time.Time) (int, error) {
	resp, err := s.kagami.ListDueFollowUps(ctx, &kagamiv1.ListDueFollowUpsRequest{OnOrBefore: date.Format(time.DateOnly)})
	if err != nil {
		return 0, fmt.Errorf("kagami ListDueFollowUps: %w", err)
	}
	return len(resp.GetJobs()) + len(resp.GetContacts()), nil
}

// DraftsWaiting counts drafts that wait for approval, up to limit.
func (s *Sources) DraftsWaiting(ctx context.Context, limit int) (int, bool, error) {
	resp, err := s.fude.ListQueue(ctx, &fudev1.ListQueueRequest{
		State: fudev1.DraftState_DRAFT_STATE_PENDING, PageSize: int32(limit), //nolint:gosec // limit is a small constant
	})
	if err != nil {
		return 0, false, fmt.Errorf("fude ListQueue: %w", err)
	}
	return len(resp.GetDrafts()), resp.GetNextPageToken() != "", nil
}

// Spend totals Claude spend from start up to end.
func (s *Sources) Spend(ctx context.Context, start, end time.Time) (int64, error) {
	resp, err := s.soroban.GetSpend(ctx, &sorobanv1.GetSpendRequest{
		From: timestamppb.New(start), To: timestamppb.New(end), GroupBy: sorobanv1.SpendGroup_SPEND_GROUP_DAY,
	})
	if err != nil {
		return 0, fmt.Errorf("soroban GetSpend: %w", err)
	}
	return resp.GetTotalMicros(), nil
}
