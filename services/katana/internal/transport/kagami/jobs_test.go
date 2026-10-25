package kagami

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
)

type fakeClient struct {
	kagamiv1.KagamiServiceClient
	job   *kagamiv1.Job
	err   error
	gotID string
}

func (f *fakeClient) GetJob(_ context.Context, in *kagamiv1.GetJobRequest, _ ...grpc.CallOption) (*kagamiv1.GetJobResponse, error) {
	f.gotID = in.GetId()
	return &kagamiv1.GetJobResponse{Job: f.job}, f.err
}

func TestJobPostingJoinsTitleAndDescriptionAndIsEmptyWithoutADescription(t *testing.T) {
	id := uuid.New()
	tests := []struct {
		name string
		job  *kagamiv1.Job
		want string
	}{
		{"both", &kagamiv1.Job{Title: "Backend Engineer", Description: " Write Go. "}, "Backend Engineer\nWrite Go."},
		{"no description", &kagamiv1.Job{Title: "Backend Engineer"}, ""},
		{"no job", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &fakeClient{job: tt.job}

			got, err := New(client).JobPosting(context.Background(), id)

			if err != nil || got != tt.want || client.gotID != id.String() {
				t.Fatalf("got %q, err %v, id %q", got, err, client.gotID)
			}
		})
	}
}

func TestJobPostingWrapsKagamisError(t *testing.T) {
	boom := errors.New("down")

	_, err := New(&fakeClient{err: boom}).JobPosting(context.Background(), uuid.New())

	if !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}
