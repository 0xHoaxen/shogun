package kagami

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"google.golang.org/grpc"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
)

type fakeClient struct {
	kagamiv1.KagamiServiceClient
	job     *kagamiv1.GetJobResponse
	contact *kagamiv1.GetContactResponse
	err     error
	gotID   string
}

func (f *fakeClient) GetJob(_ context.Context, in *kagamiv1.GetJobRequest, _ ...grpc.CallOption) (*kagamiv1.GetJobResponse, error) {
	f.gotID = in.GetId()
	return f.job, f.err
}

func (f *fakeClient) GetContact(_ context.Context, in *kagamiv1.GetContactRequest, _ ...grpc.CallOption) (*kagamiv1.GetContactResponse, error) {
	f.gotID = in.GetId()
	return f.contact, f.err
}

func TestDescribeJobSkipsEmptyFieldsAndClipsTheDescription(t *testing.T) {
	id := uuid.New()
	client := &fakeClient{job: &kagamiv1.GetJobResponse{
		Job:     &kagamiv1.Job{Title: "Backend Engineer", Description: strings.Repeat("x", maxDescriptionLen+50)},
		Company: &kagamiv1.Company{Name: "Lumen"},
	}}

	got, err := New(client).Describe(context.Background(), uuid.New(), domain.TargetJob, id)

	if err != nil || client.gotID != id.String() {
		t.Fatalf("err %v, id %q", err, client.gotID)
	}
	if !strings.Contains(got.Summary, "Role: Backend Engineer\nCompany: Lumen\nDescription: xxx") ||
		strings.Contains(got.Summary, "Location") || strings.Contains(got.Summary, "Posting") ||
		!strings.HasSuffix(got.Summary, "…") || got.ContactStatus != "" {
		t.Fatalf("got %q", got.Summary)
	}
}

func TestDescribeContactReportsItsStatusForTemplateChoice(t *testing.T) {
	client := &fakeClient{contact: &kagamiv1.GetContactResponse{Contact: &kagamiv1.Contact{
		FullName: "Priya Raman", CompanyName: "Lumen", Status: kagamiv1.ContactStatus_CONTACT_STATUS_REFERRAL_ASKED,
	}}}

	got, err := New(client).Describe(context.Background(), uuid.New(), domain.TargetContact, uuid.New())

	if err != nil || got.ContactStatus != "referral_asked" || !strings.Contains(got.Summary, "Name: Priya Raman") {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestDescribeWrapsKagamiErrorsAndIgnoresOtherTargets(t *testing.T) {
	boom := errors.New("unavailable")
	client := &fakeClient{err: boom}

	_, jobErr := New(client).Describe(context.Background(), uuid.New(), domain.TargetJob, uuid.New())
	_, contactErr := New(client).Describe(context.Background(), uuid.New(), domain.TargetContact, uuid.New())
	other, otherErr := New(client).Describe(context.Background(), uuid.New(), domain.TargetLearningActivity, uuid.New())

	if !errors.Is(jobErr, boom) || !errors.Is(contactErr, boom) {
		t.Fatalf("got %v, %v", jobErr, contactErr)
	}
	if otherErr != nil || other.Summary != "" {
		t.Fatalf("got %+v, %v; want an empty description", other, otherErr)
	}
}
