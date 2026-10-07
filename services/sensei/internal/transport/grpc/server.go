// Package grpc serves sensei.v1.SenseiService over the use cases in app.
package grpc

import (
	"context"
	"errors"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	senseiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/sensei/v1"
	"github.com/0xHoaxen/shogun/services/sensei/internal/app"
	"github.com/0xHoaxen/shogun/services/sensei/internal/domain"
)

const (
	errorDomain         = "sensei.shogun"
	reasonOwnerRequired = "OWNER_REQUIRED"
	reasonInvalidRange  = "INVALID_RANGE"
	reasonInvalidGroup  = "INVALID_GROUP_BY"
	reasonInternal      = "INTERNAL"
)

// Server implements sensei.v1.SenseiService.
type Server struct {
	senseiv1.UnimplementedSenseiServiceServer
	svc *app.Service
}

// New returns a Server that runs its calls on svc.
func New(svc *app.Service) *Server { return &Server{svc: svc} }

// GetFunnel implements sensei.v1.SenseiService.
func (s *Server) GetFunnel(ctx context.Context, req *senseiv1.GetFunnelRequest) (*senseiv1.GetFunnelResponse, error) {
	var by domain.FunnelGroup
	switch req.GetGroupBy() {
	case senseiv1.FunnelGroupBy_FUNNEL_GROUP_BY_SOURCE:
		by = domain.FunnelBySource
	case senseiv1.FunnelGroupBy_FUNNEL_GROUP_BY_MONTH:
		by = domain.FunnelByMonth
	case senseiv1.FunnelGroupBy_FUNNEL_GROUP_BY_UNSPECIFIED:
		return nil, withReason(codes.InvalidArgument, reasonInvalidGroup, "group_by is required")
	}
	res, err := s.svc.GetFunnel(ctx, req.GetFrom(), req.GetTo(), by)
	if err != nil {
		return nil, toStatus(err)
	}
	rows := make([]*senseiv1.FunnelRow, 0, len(res.Rows))
	for _, r := range res.Rows {
		rows = append(rows, funnelRow(r))
	}
	return &senseiv1.GetFunnelResponse{
		From: res.From.Format(time.DateOnly), To: res.To.Format(time.DateOnly), Rows: rows, Total: funnelRow(res.Total),
	}, nil
}

// GetOutreachStats implements sensei.v1.SenseiService.
func (s *Server) GetOutreachStats(ctx context.Context, req *senseiv1.GetOutreachStatsRequest) (*senseiv1.GetOutreachStatsResponse, error) {
	var by domain.OutreachGroup
	switch req.GetGroupBy() {
	case senseiv1.OutreachGroupBy_OUTREACH_GROUP_BY_CHANNEL:
		by = domain.OutreachByChannel
	case senseiv1.OutreachGroupBy_OUTREACH_GROUP_BY_STATUS:
		by = domain.OutreachByStatus
	case senseiv1.OutreachGroupBy_OUTREACH_GROUP_BY_UNSPECIFIED:
		return nil, withReason(codes.InvalidArgument, reasonInvalidGroup, "group_by is required")
	}
	res, err := s.svc.GetOutreachStats(ctx, req.GetFrom(), req.GetTo(), by)
	if err != nil {
		return nil, toStatus(err)
	}
	rows := make([]*senseiv1.OutreachRow, 0, len(res.Rows))
	for _, r := range res.Rows {
		rows = append(rows, outreachRow(r))
	}
	return &senseiv1.GetOutreachStatsResponse{
		From: res.From.Format(time.DateOnly), To: res.To.Format(time.DateOnly), Rows: rows, Total: outreachRow(res.Total),
	}, nil
}

func funnelRow(r domain.FunnelRow) *senseiv1.FunnelRow {
	return &senseiv1.FunnelRow{
		Key: r.Key, JobsAdded: r.JobsAdded, Applications: r.Applications, Shortlisted: r.Shortlisted, Interviews: r.Interviews,
		Offers: r.Offers, Rejections: r.Rejections, InterviewRate: r.InterviewRate(), OfferRate: r.OfferRate(),
	}
}

func outreachRow(r domain.OutreachRow) *senseiv1.OutreachRow {
	return &senseiv1.OutreachRow{Key: r.Key, Sent: r.Sent, Replied: r.Replied, ReplyRate: r.ReplyRate(), MovedIn: r.MovedIn}
}

// toStatus maps a use case error to a gRPC status carrying an ErrorInfo reason.
// Unexpected errors become Internal without their text.
func toStatus(err error) error {
	switch {
	case errors.Is(err, app.ErrNoOwner):
		return withReason(codes.PermissionDenied, reasonOwnerRequired, "call has no valid owner")
	case errors.Is(err, domain.ErrInvalidRange):
		return withReason(codes.InvalidArgument, reasonInvalidRange, err.Error())
	default:
		return withReason(codes.Internal, reasonInternal, "internal error")
	}
}

func withReason(code codes.Code, reason, msg string) error {
	st := status.New(code, msg)
	detailed, err := st.WithDetails(&errdetails.ErrorInfo{Reason: reason, Domain: errorDomain})
	if err != nil {
		return st.Err()
	}
	return detailed.Err()
}
