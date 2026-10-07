package dojo_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	dojov1 "github.com/0xHoaxen/shogun/gen/go/shogun/dojo/v1"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
	"github.com/0xHoaxen/shogun/services/fude/internal/transport/dojo"
)

// fakeDojo answers GetActivity and GetItem; the other methods are the embedded
// nil interface and are never called.
type fakeDojo struct {
	dojov1.DojoServiceClient
	activity    *dojov1.GetActivityResponse
	activityErr error
	item        *dojov1.GetItemResponse
	itemErr     error
	asked       []string
}

func (f *fakeDojo) GetActivity(_ context.Context, in *dojov1.GetActivityRequest, _ ...grpc.CallOption) (*dojov1.GetActivityResponse, error) {
	f.asked = append(f.asked, "activity:"+in.GetId())
	return f.activity, f.activityErr
}

func (f *fakeDojo) GetItem(_ context.Context, in *dojov1.GetItemRequest, _ ...grpc.CallOption) (*dojov1.GetItemResponse, error) {
	f.asked = append(f.asked, "item:"+in.GetId())
	return f.item, f.itemErr
}

// nextSource records the targets it is asked about.
type nextSource struct{ got []domain.TargetType }

func (n *nextSource) Describe(_ context.Context, _ uuid.UUID, target domain.TargetType, _ uuid.UUID) (app.TargetContext, error) {
	n.got = append(n.got, target)
	return app.TargetContext{Summary: "from next"}, nil
}

func TestDescribeReadsAnActivityWithItsItem(t *testing.T) {
	id := uuid.New()
	client := &fakeDojo{activity: &dojov1.GetActivityResponse{
		Activity: &dojov1.Activity{Id: id.String(), Summary: "built a worker pool", Minutes: 45, OccurredOn: "2026-10-07", Tags: []string{"go", "concurrency"}},
		Item:     &dojov1.Item{Title: "Go course", Kind: dojov1.ItemKind_ITEM_KIND_COURSE, Insight: "channels first"},
	}}
	next := &nextSource{}

	got, err := dojo.New(client, next).Describe(context.Background(), uuid.New(), domain.TargetLearningActivity, id)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	for _, want := range []string{
		"Learned: built a worker pool", "Date: 2026-10-07", "Minutes spent: 45", "Tags: go, concurrency",
		"Part of: Go course", "Kind: course", "Takeaway: channels first",
	} {
		if !strings.Contains(got.Summary, want) {
			t.Errorf("summary lacks %q:\n%s", want, got.Summary)
		}
	}
	if len(next.got) != 0 || len(client.asked) != 1 {
		t.Fatalf("next asked %v, dojo asked %v; want dojo once and next never", next.got, client.asked)
	}
}

func TestDescribeLeavesOutWhatAnActivityDoesNotHave(t *testing.T) {
	client := &fakeDojo{activity: &dojov1.GetActivityResponse{Activity: &dojov1.Activity{Summary: "listened to a podcast"}}}

	got, err := dojo.New(client, &nextSource{}).Describe(context.Background(), uuid.New(), domain.TargetLearningActivity, uuid.New())

	if err != nil || got.Summary != "Learned: listened to a podcast" {
		t.Fatalf("summary %q, err %v; want only the summary line", got.Summary, err)
	}
}

func TestDescribeFallsBackToAFinishedItem(t *testing.T) {
	id := uuid.New()
	client := &fakeDojo{
		activityErr: status.Error(codes.NotFound, "activity not found"),
		item: &dojov1.GetItemResponse{Item: &dojov1.Item{
			Title: "Designing Data-Intensive Applications", Kind: dojov1.ItemKind_ITEM_KIND_BOOK, Url: "https://example.com/ddia",
			StartedOn: "2026-08-01", CompletedOn: "2026-10-07", Insight: "logs are everywhere",
		}},
	}

	got, err := dojo.New(client, &nextSource{}).Describe(context.Background(), uuid.New(), domain.TargetLearningActivity, id)
	if err != nil {
		t.Fatalf("Describe: %v", err)
	}
	for _, want := range []string{
		"Finished: Designing Data-Intensive Applications", "Kind: book", "Link: https://example.com/ddia",
		"Started on: 2026-08-01", "Completed on: 2026-10-07", "Takeaway: logs are everywhere",
	} {
		if !strings.Contains(got.Summary, want) {
			t.Errorf("summary lacks %q:\n%s", want, got.Summary)
		}
	}
	if len(client.asked) != 2 || client.asked[1] != "item:"+id.String() {
		t.Fatalf("dojo asked %v, want the activity and then the item", client.asked)
	}
}

func TestDescribeFailsWhenDojoDoes(t *testing.T) {
	errDown := status.Error(codes.Unavailable, "down")
	tests := []struct {
		name   string
		client *fakeDojo
	}{
		{"activity lookup fails", &fakeDojo{activityErr: errDown}},
		{"item lookup fails", &fakeDojo{activityErr: status.Error(codes.NotFound, "no"), itemErr: errDown}},
		{"neither exists", &fakeDojo{activityErr: status.Error(codes.NotFound, "no"), itemErr: status.Error(codes.NotFound, "no")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := dojo.New(tt.client, &nextSource{}).Describe(context.Background(), uuid.New(), domain.TargetLearningActivity, uuid.New())

			if err == nil || errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want a failure so the generation job retries or fails", err)
			}
		})
	}
}

func TestDescribeHandsOtherTargetsToTheNextSource(t *testing.T) {
	client := &fakeDojo{}
	next := &nextSource{}

	got, err := dojo.New(client, next).Describe(context.Background(), uuid.New(), domain.TargetJob, uuid.New())

	if err != nil || got.Summary != "from next" || len(next.got) != 1 || next.got[0] != domain.TargetJob || len(client.asked) != 0 {
		t.Fatalf("got %+v, %v; next %v, dojo %v", got, err, next.got, client.asked)
	}
}
