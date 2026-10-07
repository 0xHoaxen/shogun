package connectapi

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"google.golang.org/grpc"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	senseiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/sensei/v1"
)

// InsightsBackend is the part of sensei's client InsightsServer uses.
type InsightsBackend interface {
	GetFunnel(ctx context.Context, in *senseiv1.GetFunnelRequest, opts ...grpc.CallOption) (*senseiv1.GetFunnelResponse, error)
	GetOutreachStats(ctx context.Context, in *senseiv1.GetOutreachStatsRequest, opts ...grpc.CallOption) (*senseiv1.GetOutreachStatsResponse, error)
}

// InsightsServer implements shogun.api.v1.InsightsService on top of sensei.
type InsightsServer struct {
	sensei InsightsBackend
	log    *slog.Logger
}

var _ apiv1connect.InsightsServiceHandler = (*InsightsServer)(nil)

// NewInsightsServer returns an InsightsServer that calls sensei through backend.
func NewInsightsServer(backend InsightsBackend, log *slog.Logger) *InsightsServer {
	return &InsightsServer{sensei: backend, log: log}
}

// The API's group names carry an INSIGHTS_ prefix and sensei's differ, so they
// are mapped by case: a value one side lacks becomes unspecified instead of
// silently turning into a different one.

func funnelGroupToSensei(g apiv1.InsightsFunnelGroup) senseiv1.FunnelGroupBy {
	switch g {
	case apiv1.InsightsFunnelGroup_INSIGHTS_FUNNEL_GROUP_SOURCE:
		return senseiv1.FunnelGroupBy_FUNNEL_GROUP_BY_SOURCE
	case apiv1.InsightsFunnelGroup_INSIGHTS_FUNNEL_GROUP_MONTH:
		return senseiv1.FunnelGroupBy_FUNNEL_GROUP_BY_MONTH
	case apiv1.InsightsFunnelGroup_INSIGHTS_FUNNEL_GROUP_UNSPECIFIED:
		return senseiv1.FunnelGroupBy_FUNNEL_GROUP_BY_UNSPECIFIED
	}
	return senseiv1.FunnelGroupBy_FUNNEL_GROUP_BY_UNSPECIFIED
}

func outreachGroupToSensei(g apiv1.InsightsOutreachGroup) senseiv1.OutreachGroupBy {
	switch g {
	case apiv1.InsightsOutreachGroup_INSIGHTS_OUTREACH_GROUP_CHANNEL:
		return senseiv1.OutreachGroupBy_OUTREACH_GROUP_BY_CHANNEL
	case apiv1.InsightsOutreachGroup_INSIGHTS_OUTREACH_GROUP_STATUS:
		return senseiv1.OutreachGroupBy_OUTREACH_GROUP_BY_STATUS
	case apiv1.InsightsOutreachGroup_INSIGHTS_OUTREACH_GROUP_UNSPECIFIED:
		return senseiv1.OutreachGroupBy_OUTREACH_GROUP_BY_UNSPECIFIED
	}
	return senseiv1.OutreachGroupBy_OUTREACH_GROUP_BY_UNSPECIFIED
}

func funnelRowToAPI(r *senseiv1.FunnelRow) *apiv1.InsightsFunnelRow {
	return &apiv1.InsightsFunnelRow{
		Key: r.GetKey(), JobsAdded: r.GetJobsAdded(), Applications: r.GetApplications(), Shortlisted: r.GetShortlisted(),
		Interviews: r.GetInterviews(), Offers: r.GetOffers(), Rejections: r.GetRejections(),
		InterviewRate: r.GetInterviewRate(), OfferRate: r.GetOfferRate(),
	}
}

func outreachRowToAPI(r *senseiv1.OutreachRow) *apiv1.InsightsOutreachRow {
	return &apiv1.InsightsOutreachRow{
		Key: r.GetKey(), Sent: r.GetSent(), Replied: r.GetReplied(), ReplyRate: r.GetReplyRate(), MovedIn: r.GetMovedIn(),
	}
}

// GetInsightsFunnel returns the job funnel.
func (s *InsightsServer) GetInsightsFunnel(
	ctx context.Context, req *connect.Request[apiv1.GetInsightsFunnelRequest],
) (*connect.Response[apiv1.GetInsightsFunnelResponse], error) {
	in := req.Msg
	resp, err := s.sensei.GetFunnel(ctx, &senseiv1.GetFunnelRequest{
		From: in.GetFrom(), To: in.GetTo(), GroupBy: funnelGroupToSensei(in.GetGroupBy()),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	rows := make([]*apiv1.InsightsFunnelRow, 0, len(resp.GetRows()))
	for _, r := range resp.GetRows() {
		rows = append(rows, funnelRowToAPI(r))
	}
	return connect.NewResponse(&apiv1.GetInsightsFunnelResponse{
		From: resp.GetFrom(), To: resp.GetTo(), Rows: rows, Total: funnelRowToAPI(resp.GetTotal()),
	}), nil
}

// GetInsightsOutreach returns outreach sent and replied.
func (s *InsightsServer) GetInsightsOutreach(
	ctx context.Context, req *connect.Request[apiv1.GetInsightsOutreachRequest],
) (*connect.Response[apiv1.GetInsightsOutreachResponse], error) {
	in := req.Msg
	resp, err := s.sensei.GetOutreachStats(ctx, &senseiv1.GetOutreachStatsRequest{
		From: in.GetFrom(), To: in.GetTo(), GroupBy: outreachGroupToSensei(in.GetGroupBy()),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	rows := make([]*apiv1.InsightsOutreachRow, 0, len(resp.GetRows()))
	for _, r := range resp.GetRows() {
		rows = append(rows, outreachRowToAPI(r))
	}
	return connect.NewResponse(&apiv1.GetInsightsOutreachResponse{
		From: resp.GetFrom(), To: resp.GetTo(), Rows: rows, Total: outreachRowToAPI(resp.GetTotal()),
	}), nil
}
