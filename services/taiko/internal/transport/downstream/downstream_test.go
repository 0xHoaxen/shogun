package downstream_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"

	fudev1 "github.com/0xHoaxen/shogun/gen/go/shogun/fude/v1"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	sorobanv1 "github.com/0xHoaxen/shogun/gen/go/shogun/soroban/v1"
	"github.com/0xHoaxen/shogun/services/taiko/internal/transport/downstream"
)

type fakeKagami struct {
	req  *kagamiv1.ListDueFollowUpsRequest
	resp *kagamiv1.ListDueFollowUpsResponse
	err  error
}

func (f *fakeKagami) ListDueFollowUps(_ context.Context, in *kagamiv1.ListDueFollowUpsRequest, _ ...grpc.CallOption) (*kagamiv1.ListDueFollowUpsResponse, error) {
	f.req = in
	return f.resp, f.err
}

type fakeFude struct {
	req  *fudev1.ListQueueRequest
	resp *fudev1.ListQueueResponse
	err  error
}

func (f *fakeFude) ListQueue(_ context.Context, in *fudev1.ListQueueRequest, _ ...grpc.CallOption) (*fudev1.ListQueueResponse, error) {
	f.req = in
	return f.resp, f.err
}

type fakeSoroban struct {
	req  *sorobanv1.GetSpendRequest
	resp *sorobanv1.GetSpendResponse
	err  error
}

func (f *fakeSoroban) GetSpend(_ context.Context, in *sorobanv1.GetSpendRequest, _ ...grpc.CallOption) (*sorobanv1.GetSpendResponse, error) {
	f.req = in
	return f.resp, f.err
}

func TestFollowUpsDueCountsJobsAndContactsAndAsksForTheDate(t *testing.T) {
	k := &fakeKagami{resp: &kagamiv1.ListDueFollowUpsResponse{
		Jobs: []*kagamiv1.Job{{}, {}}, Contacts: []*kagamiv1.Contact{{}},
	}}
	s := downstream.New(k, &fakeFude{}, &fakeSoroban{})

	n, err := s.FollowUpsDue(context.Background(), time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC))

	if err != nil || n != 3 || k.req.GetOnOrBefore() != "2026-10-07" {
		t.Fatalf("n %d, err %v, asked %q", n, err, k.req.GetOnOrBefore())
	}
}

func TestDraftsWaitingAsksForPendingAndReportsAMoreToCome(t *testing.T) {
	f := &fakeFude{resp: &fudev1.ListQueueResponse{Drafts: []*fudev1.Draft{{}, {}}, NextPageToken: "more"}}
	s := downstream.New(&fakeKagami{}, f, &fakeSoroban{})

	n, capped, err := s.DraftsWaiting(context.Background(), 200)

	if err != nil || n != 2 || !capped {
		t.Fatalf("n %d, capped %v, err %v", n, capped, err)
	}
	if f.req.GetState() != fudev1.DraftState_DRAFT_STATE_PENDING || f.req.GetPageSize() != 200 {
		t.Fatalf("asked %+v", f.req)
	}
}

func TestDraftsWaitingOnTheLastPageIsNotCapped(t *testing.T) {
	s := downstream.New(&fakeKagami{}, &fakeFude{resp: &fudev1.ListQueueResponse{Drafts: []*fudev1.Draft{{}}}}, &fakeSoroban{})

	_, capped, err := s.DraftsWaiting(context.Background(), 200)

	if err != nil || capped {
		t.Fatalf("capped %v, err %v", capped, err)
	}
}

func TestSpendTotalsTheRangeByDay(t *testing.T) {
	g := &fakeSoroban{resp: &sorobanv1.GetSpendResponse{TotalMicros: 6_400_000}}
	s := downstream.New(&fakeKagami{}, &fakeFude{}, g)
	start := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)

	micros, err := s.Spend(context.Background(), start, start.AddDate(0, 0, 1))

	if err != nil || micros != 6_400_000 {
		t.Fatalf("micros %d, err %v", micros, err)
	}
	if !g.req.GetFrom().AsTime().Equal(start) || !g.req.GetTo().AsTime().Equal(start.AddDate(0, 0, 1)) ||
		g.req.GetGroupBy() != sorobanv1.SpendGroup_SPEND_GROUP_DAY {
		t.Fatalf("asked %+v", g.req)
	}
}

func TestFailuresNameTheServiceAndKeepTheCause(t *testing.T) {
	boom := errors.New("unavailable")
	s := downstream.New(&fakeKagami{err: boom}, &fakeFude{err: boom}, &fakeSoroban{err: boom})
	ctx := context.Background()

	_, errFollow := s.FollowUpsDue(ctx, time.Now())
	_, _, errDrafts := s.DraftsWaiting(ctx, 1)
	_, errSpend := s.Spend(ctx, time.Now(), time.Now())

	for name, err := range map[string]error{"kagami": errFollow, "fude": errDrafts, "soroban": errSpend} {
		if !errors.Is(err, boom) {
			t.Errorf("%s: %v does not wrap the cause", name, err)
		}
	}
}
