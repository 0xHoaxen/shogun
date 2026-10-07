package connectapi_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	senseiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/sensei/v1"
	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/app/apptest"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/connectapi"
)

type fakeSensei struct {
	err      error
	funnel   *senseiv1.GetFunnelRequest
	outreach *senseiv1.GetOutreachStatsRequest
}

func (f *fakeSensei) GetFunnel(_ context.Context, in *senseiv1.GetFunnelRequest, _ ...grpc.CallOption) (*senseiv1.GetFunnelResponse, error) {
	f.funnel = in
	return &senseiv1.GetFunnelResponse{
		From: "2026-10-01", To: "2026-10-07",
		Rows:  []*senseiv1.FunnelRow{{Key: "linkedin", JobsAdded: 10, Applications: 8, Shortlisted: 3, Interviews: 2, Offers: 1, Rejections: 4, InterviewRate: 0.25, OfferRate: 0.125}},
		Total: &senseiv1.FunnelRow{Key: "total", Applications: 8},
	}, f.err
}

func (f *fakeSensei) GetOutreachStats(_ context.Context, in *senseiv1.GetOutreachStatsRequest, _ ...grpc.CallOption) (*senseiv1.GetOutreachStatsResponse, error) {
	f.outreach = in
	return &senseiv1.GetOutreachStatsResponse{
		From: "2026-10-01", To: "2026-10-07",
		Rows:  []*senseiv1.OutreachRow{{Key: "email", Sent: 8, Replied: 4, ReplyRate: 0.5, MovedIn: 9}},
		Total: &senseiv1.OutreachRow{Key: "total", Sent: 8, Replied: 4, ReplyRate: 0.5},
	}, f.err
}

func newInsightsHarness(t *testing.T) (*fakeSensei, apiv1connect.InsightsServiceClient, string) {
	t.Helper()
	now := time.Date(2026, 10, 7, 9, 0, 0, 0, time.UTC)
	auth, err := app.NewAuth(apptest.NewMemSessions(),
		app.AuthConfig{AllowedEmails: []string{ownerEmail}, SessionTTL: sessionTTL},
		app.WithClock(func() time.Time { return now }))
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	token, _, err := auth.StartSession(context.Background(), app.Claims{Subject: "sub-1", Email: ownerEmail, EmailVerified: true})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	log := slog.New(slog.DiscardHandler)
	fake := &fakeSensei{}
	mux := http.NewServeMux()
	mux.Handle(apiv1connect.NewInsightsServiceHandler(connectapi.NewInsightsServer(fake, log),
		connect.WithInterceptors(connectapi.NewSessionInterceptor(connectapi.InterceptorConfig{Auth: auth, Log: log}))))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return fake, apiv1connect.NewInsightsServiceClient(srv.Client(), srv.URL), token
}

func TestGetInsightsFunnelMapsTheRequestAndTheRows(t *testing.T) {
	fake, client, token := newInsightsHarness(t)

	resp, err := client.GetInsightsFunnel(context.Background(), withCookie(connect.NewRequest(&apiv1.GetInsightsFunnelRequest{
		From: "2026-10-01", To: "2026-10-07", GroupBy: apiv1.InsightsFunnelGroup_INSIGHTS_FUNNEL_GROUP_MONTH,
	}), token))

	if err != nil || fake.funnel.GetFrom() != "2026-10-01" || fake.funnel.GetTo() != "2026-10-07" || fake.funnel.GetGroupBy() != senseiv1.FunnelGroupBy_FUNNEL_GROUP_BY_MONTH {
		t.Fatalf("err %v, sensei got %+v", err, fake.funnel)
	}
	row := resp.Msg.GetRows()[0]
	if row.GetKey() != "linkedin" || row.GetJobsAdded() != 10 || row.GetApplications() != 8 || row.GetShortlisted() != 3 || row.GetInterviews() != 2 ||
		row.GetOffers() != 1 || row.GetRejections() != 4 || row.GetInterviewRate() != 0.25 || row.GetOfferRate() != 0.125 ||
		resp.Msg.GetTotal().GetApplications() != 8 || resp.Msg.GetFrom() != "2026-10-01" {
		t.Fatalf("got %+v", resp.Msg)
	}
}

func TestGetInsightsOutreachMapsTheRequestAndTheRows(t *testing.T) {
	fake, client, token := newInsightsHarness(t)

	resp, err := client.GetInsightsOutreach(context.Background(), withCookie(connect.NewRequest(&apiv1.GetInsightsOutreachRequest{
		GroupBy: apiv1.InsightsOutreachGroup_INSIGHTS_OUTREACH_GROUP_STATUS,
	}), token))

	if err != nil || fake.outreach.GetGroupBy() != senseiv1.OutreachGroupBy_OUTREACH_GROUP_BY_STATUS {
		t.Fatalf("err %v, sensei got %+v", err, fake.outreach)
	}
	row := resp.Msg.GetRows()[0]
	if row.GetKey() != "email" || row.GetSent() != 8 || row.GetReplied() != 4 || row.GetReplyRate() != 0.5 || row.GetMovedIn() != 9 || resp.Msg.GetTotal().GetSent() != 8 {
		t.Fatalf("got %+v", resp.Msg)
	}
}

func TestAnUnspecifiedGroupReachesSenseiAsUnspecifiedSoItRefusesIt(t *testing.T) {
	fake, client, token := newInsightsHarness(t)

	_, _ = client.GetInsightsFunnel(context.Background(), withCookie(connect.NewRequest(&apiv1.GetInsightsFunnelRequest{}), token))
	_, _ = client.GetInsightsOutreach(context.Background(), withCookie(connect.NewRequest(&apiv1.GetInsightsOutreachRequest{}), token))

	if fake.funnel.GetGroupBy() != senseiv1.FunnelGroupBy_FUNNEL_GROUP_BY_UNSPECIFIED || fake.outreach.GetGroupBy() != senseiv1.OutreachGroupBy_OUTREACH_GROUP_BY_UNSPECIFIED {
		t.Fatalf("funnel %v, outreach %v", fake.funnel.GetGroupBy(), fake.outreach.GetGroupBy())
	}
}

func TestInsightsErrorsKeepTheirReasonsAndHideInternals(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   connect.Code
		wantReason string
	}{
		{"bad range", withInfo(codes.InvalidArgument, "INVALID_RANGE"), connect.CodeInvalidArgument, "INVALID_RANGE"},
		{"no group", withInfo(codes.InvalidArgument, "INVALID_GROUP_BY"), connect.CodeInvalidArgument, "INVALID_GROUP_BY"},
		{"busy", withInfo(codes.Unavailable, "X"), connect.CodeUnavailable, "UNAVAILABLE"},
		{"internals", status.Error(codes.Internal, "secret internals"), connect.CodeInternal, "INTERNAL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake, client, token := newInsightsHarness(t)
			fake.err = tt.err

			_, err := client.GetInsightsFunnel(context.Background(), withCookie(connect.NewRequest(&apiv1.GetInsightsFunnelRequest{}), token))

			code, reason := codeAndReason(t, err)
			if code != tt.wantCode || reason != tt.wantReason || (err != nil && strings.Contains(err.Error(), "secret internals")) {
				t.Fatalf("got %s %q (%v), want %s %q", code, reason, err, tt.wantCode, tt.wantReason)
			}
		})
	}
}

func TestInsightsCallsRequireASession(t *testing.T) {
	fake, client, _ := newInsightsHarness(t)

	_, err := client.GetInsightsFunnel(context.Background(), connect.NewRequest(&apiv1.GetInsightsFunnelRequest{}))

	if code, _ := codeAndReason(t, err); code != connect.CodeUnauthenticated || fake.funnel != nil {
		t.Fatalf("got %v, sensei called: %v", err, fake.funnel != nil)
	}
}
