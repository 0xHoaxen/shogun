package grpc

import (
	"context"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/services/kagami/internal/app"
)

// ImportContacts implements kagami.v1.KagamiService.
func (s *Server) ImportContacts(ctx context.Context, req *kagamiv1.ImportContactsRequest) (*kagamiv1.ImportContactsResponse, error) {
	res, err := s.svc.ImportContacts(ctx, app.ImportContactsInput{
		Filename: req.GetFilename(), CSV: req.GetCsv(), DryRun: req.GetDryRun(), Reshape: req.GetReshape(),
		Mappings: columnMappings(req.GetMappings()), DateOrder: req.GetDateOrder(), Selection: rowSelection(req.GetSelection()),
	})
	if err != nil {
		return nil, toStatus(err)
	}
	rowErrors := make([]*kagamiv1.ImportRowError, 0, len(res.Report.Errors))
	for _, e := range res.Report.Errors {
		rowErrors = append(rowErrors, &kagamiv1.ImportRowError{Row: e.Row, Column: e.Column, Message: e.Message})
	}
	mappings := make([]*kagamiv1.ColumnMapping, 0, len(res.Mappings))
	for _, m := range res.Mappings {
		mappings = append(mappings, &kagamiv1.ColumnMapping{Target: m.Target, Sources: m.Sources})
	}
	rows := make([]*kagamiv1.ImportPreviewRow, 0, len(res.Rows))
	for _, r := range res.Rows {
		rows = append(rows, &kagamiv1.ImportPreviewRow{
			Row: r.Row, FullName: r.FullName, Role: r.Role, Company: r.Company, Email: r.Email,
			Relationship: r.Relationship, RelationshipGuessed: r.RelationshipGuessed,
			Duplicate: r.Duplicate, DuplicateContactId: r.DuplicateContactID, DuplicateReason: r.DuplicateReason,
			RepeatsRow: r.RepeatsRow, Problem: r.Problem,
		})
	}
	return &kagamiv1.ImportContactsResponse{
		Report: &kagamiv1.ImportReport{
			RowsTotal: res.Report.RowsTotal, RowsCreated: res.Report.RowsCreated,
			RowsUpdated: res.Report.RowsUpdated, RowsFailed: res.Report.RowsFailed,
			RowsSkipped: res.Report.RowsSkipped, Errors: rowErrors,
		},
		ImportId:      res.ImportID,
		ShapedCsv:     res.ShapedCSV,
		Mappings:      mappings,
		DateOrder:     res.DateOrder,
		Rows:          rows,
		RowsTruncated: res.PreviewTruncated,
	}, nil
}

func columnMappings(in []*kagamiv1.ColumnMapping) []app.ColumnMapping {
	out := make([]app.ColumnMapping, 0, len(in))
	for _, m := range in {
		out = append(out, app.ColumnMapping{Target: m.GetTarget(), Sources: m.GetSources()})
	}
	return out
}

// rowSelection is nil when the request sent none, which means every row.
func rowSelection(in *kagamiv1.RowSelection) *app.RowSelection {
	if in == nil {
		return nil
	}
	return &app.RowSelection{Rows: in.GetRows()}
}
