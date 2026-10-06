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
	res *kagamiv1.FindMailLinksResponse
	err error
	got *kagamiv1.FindMailLinksRequest
}

func (f *fakeClient) FindMailLinks(_ context.Context, in *kagamiv1.FindMailLinksRequest, _ ...grpc.CallOption) (*kagamiv1.FindMailLinksResponse, error) {
	f.got = in
	return f.res, f.err
}

func TestFindLinksPassesTheSenderAndURLsAndParsesTheIDs(t *testing.T) {
	contact, job := uuid.New(), uuid.New()
	client := &fakeClient{res: &kagamiv1.FindMailLinksResponse{ContactId: contact.String(), JobId: job.String()}}

	got, err := New(client).FindLinks(context.Background(), "hr@lumen.example", []string{"https://lumen.example/j"})

	if err != nil || got.ContactID == nil || *got.ContactID != contact || got.JobID == nil || *got.JobID != job {
		t.Fatalf("got %+v, %v", got, err)
	}
	if client.got.GetFromEmail() != "hr@lumen.example" || len(client.got.GetUrls()) != 1 {
		t.Fatalf("sent %+v", client.got)
	}
}

func TestFindLinksReturnsNilForWhatKagamiDidNotFind(t *testing.T) {
	got, err := New(&fakeClient{res: &kagamiv1.FindMailLinksResponse{}}).FindLinks(context.Background(), "a@b.example", nil)

	if err != nil || got.ContactID != nil || got.JobID != nil {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestFindLinksReportsFailuresAndBadIDs(t *testing.T) {
	boom := errors.New("unavailable")
	tests := []struct {
		name   string
		client *fakeClient
		is     error
	}{
		{"kagami fails", &fakeClient{err: boom}, boom},
		{"a malformed contact id", &fakeClient{res: &kagamiv1.FindMailLinksResponse{ContactId: "nope"}}, nil},
		{"a malformed job id", &fakeClient{res: &kagamiv1.FindMailLinksResponse{JobId: "nope"}}, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(tt.client).FindLinks(context.Background(), "a@b.example", nil)

			if err == nil || (tt.is != nil && !errors.Is(err, tt.is)) {
				t.Fatalf("got %v", err)
			}
		})
	}
}
