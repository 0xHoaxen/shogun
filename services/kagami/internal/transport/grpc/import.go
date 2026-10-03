package grpc

import (
	"context"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/services/kagami/internal/app"
)

// ImportContacts implements kagami.v1.KagamiService.
func (s *Server) ImportContacts(ctx context.Context, req *kagamiv1.ImportContactsRequest) (*kagamiv1.ImportContactsResponse, error) {
	res, err := s.svc.ImportContacts(ctx, app.ImportContactsInput{
		Filename: req.GetFilename(), CSV: req.GetCsv(), DryRun: req.GetDryRun(),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	rowErrors := make([]*kagamiv1.ImportRowError, 0, len(res.Report.Errors))
	for _, e := range res.Report.Errors {
		rowErrors = append(rowErrors, &kagamiv1.ImportRowError{Row: e.Row, Column: e.Column, Message: e.Message})
	}
	return &kagamiv1.ImportContactsResponse{
		Report: &kagamiv1.ImportReport{
			RowsTotal: res.Report.RowsTotal, RowsCreated: res.Report.RowsCreated,
			RowsUpdated: res.Report.RowsUpdated, RowsFailed: res.Report.RowsFailed, Errors: rowErrors,
		},
		ImportId: res.ImportID,
	}, nil
}
