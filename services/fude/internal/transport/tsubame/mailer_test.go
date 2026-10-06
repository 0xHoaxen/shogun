package tsubame

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	tsubamev1 "github.com/0xHoaxen/shogun/gen/go/shogun/tsubame/v1"
	"github.com/0xHoaxen/shogun/services/fude/internal/app"
)

type fakeClient struct {
	tsubamev1.TsubameServiceClient
	err error
	got *tsubamev1.SendRequest
}

func (f *fakeClient) Send(_ context.Context, in *tsubamev1.SendRequest, _ ...grpc.CallOption) (*tsubamev1.SendResponse, error) {
	f.got = in
	return &tsubamev1.SendResponse{ProviderMessageId: "m1"}, f.err
}

func refusal(code codes.Code, reason string) error {
	st, err := status.New(code, "msg").WithDetails(&errdetails.ErrorInfo{Reason: reason, Domain: "tsubame.shogun"})
	if err != nil {
		panic(err)
	}
	return st.Err()
}

func TestSendPassesTheRequestThrough(t *testing.T) {
	client := &fakeClient{}
	draft := uuid.New()

	err := New(client).Send(context.Background(), app.SendRequest{
		Token: "v4.public.tok", DraftID: draft, Version: 3, To: []string{"a@b.example"}, Subject: "s", Body: "b", ContactID: "c1", JobID: "j1",
	})

	got := client.got
	if err != nil || got.GetHanko() != "v4.public.tok" || got.GetDraftId() != draft.String() || got.GetVersion() != 3 ||
		got.GetTo()[0] != "a@b.example" || got.GetSubject() != "s" || got.GetBody() != "b" || got.GetContactId() != "c1" || got.GetJobId() != "j1" {
		t.Fatalf("err %v, sent %+v", err, got)
	}
}

func TestSendClassifiesWhatTsubameSaid(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want error // nil means an unknown outcome
	}{
		{"approval already used", refusal(codes.PermissionDenied, "APPROVAL_ALREADY_USED"), app.ErrMailInFlight},
		{"version already sent", refusal(codes.PermissionDenied, "ALREADY_SENT"), app.ErrMailInFlight},
		{"not approved", refusal(codes.PermissionDenied, "NOT_APPROVED"), app.ErrMailRefused},
		{"invalid send", refusal(codes.InvalidArgument, "INVALID_SEND"), app.ErrMailRefused},
		{"no mail account", refusal(codes.FailedPrecondition, "NO_MAIL_ACCOUNT"), app.ErrMailRefused},
		{"the provider failed", refusal(codes.FailedPrecondition, "SEND_FAILED"), app.ErrMailFailed},
		{"unavailable", status.Error(codes.Unavailable, "down"), app.ErrMailUnreachable},
		{"deadline exceeded is not proof of anything", status.Error(codes.DeadlineExceeded, "slow"), nil},
		{"internal error is not proof of anything", status.Error(codes.Internal, "boom"), nil},
		{"an unrecognised reason", refusal(codes.PermissionDenied, "SOMETHING_NEW"), nil},
		{"a plain error", errors.New("connection reset"), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := New(&fakeClient{err: tt.err}).Send(context.Background(), app.SendRequest{})

			if err == nil {
				t.Fatal("want an error")
			}
			known := errors.Is(err, app.ErrMailInFlight) || errors.Is(err, app.ErrMailRefused) ||
				errors.Is(err, app.ErrMailFailed) || errors.Is(err, app.ErrMailUnreachable)
			if tt.want == nil && known {
				t.Fatalf("an unknown outcome was classified as %v", err)
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("got %v, want %v", err, tt.want)
			}
		})
	}
}

func TestSendErrorsNeverCarryTheTokenOrTheMail(t *testing.T) {
	err := New(&fakeClient{err: status.Error(codes.Internal, "boom")}).Send(context.Background(),
		app.SendRequest{Token: "v4.public.secret-token", Subject: "private subject", Body: "private body"})

	for _, secret := range []string{"secret-token", "private subject", "private body"} {
		if err != nil && strings.Contains(err.Error(), secret) {
			t.Fatalf("error %q leaks %q", err, secret)
		}
	}
}
