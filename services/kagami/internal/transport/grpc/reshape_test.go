package grpc_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/kagami/internal/app"
)

// stubModel answers every reshape with text or err.
type stubModel struct {
	text string
	err  error
}

func (m stubModel) Complete(context.Context, string, llm.Request) (llm.Response, error) {
	return llm.Response{Text: m.text}, m.err
}

const googleExport = "Given Name,Family Name,E-mail 1 - Value,Organization Name\n" +
	"Priya,Raman,priya@lumen.example,Lumen\n" +
	"Arjun,Mehta,arjun@corvid.example,Corvid\n"

const googleAnswer = `{"columns":[{"target":"full_name","sources":["Given Name","Family Name"]},` +
	`{"target":"email","sources":["E-mail 1 - Value"]},{"target":"company","sources":["Organization Name"]}]}`

func TestImportReshapePreviewsThenImportsTheShapedSheet(t *testing.T) {
	// Arrange
	h := newHarness(t, app.WithReshaper(stubModel{text: googleAnswer}))

	// Act
	preview, err := h.client.ImportContacts(h.ctx(t), &kagamiv1.ImportContactsRequest{
		Filename: "google.csv", Csv: []byte(googleExport), DryRun: true, Reshape: true,
	})
	// Assert
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if preview.GetReport().GetRowsCreated() != 2 || len(preview.GetShapedCsv()) == 0 {
		t.Fatalf("preview = %+v", preview)
	}
	if m := preview.GetMappings(); len(m) != 3 || m[0].GetTarget() != "full_name" || len(m[0].GetSources()) != 2 {
		t.Fatalf("mappings = %+v", m)
	}
	if n := h.rowCount(t, "contacts"); n != 0 {
		t.Fatalf("preview wrote %d contacts", n)
	}

	res := h.importCSV(t, string(preview.GetShapedCsv()), false)
	if res.GetReport().GetRowsCreated() != 2 || len(res.GetShapedCsv()) != 0 {
		t.Fatalf("import = %+v", res)
	}
	list, err := h.client.ListContacts(h.ctx(t), &kagamiv1.ListContactsRequest{Query: "Priya Raman"})
	if err != nil || len(list.GetContacts()) != 1 || list.GetContacts()[0].GetEmail() != "priya@lumen.example" {
		t.Fatalf("shaped contact not stored: %+v, %v", list.GetContacts(), err)
	}
}

func TestImportReshapeFailures(t *testing.T) {
	tests := []struct {
		name   string
		opts   []app.Option
		code   codes.Code
		reason string
	}{
		{"no name column", []app.Option{app.WithReshaper(stubModel{text: `{"columns":[]}`})}, codes.FailedPrecondition, "IMPORT_NO_NAME_COLUMN"},
		{"bad answer", []app.Option{app.WithReshaper(stubModel{text: "no idea"})}, codes.FailedPrecondition, "IMPORT_RESHAPE_FAILED"},
		{"no provider", []app.Option{app.WithReshaper(stubModel{err: llm.ErrProviderNotConfigured})}, codes.FailedPrecondition, "AI_PROVIDER_NOT_CONFIGURED"},
		{"budget spent", []app.Option{app.WithReshaper(stubModel{err: llm.ErrBudgetExhausted})}, codes.ResourceExhausted, "BUDGET_EXHAUSTED"},
		{"metering down", []app.Option{app.WithReshaper(stubModel{err: llm.ErrMeteringUnavailable})}, codes.Unavailable, "METERING_UNAVAILABLE"},
		{"no model", nil, codes.Unavailable, "AI_UNAVAILABLE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, tt.opts...)
			_, err := h.client.ImportContacts(h.ctx(t), &kagamiv1.ImportContactsRequest{
				Csv: []byte(googleExport), DryRun: true, Reshape: true,
			})
			requireStatus(t, err, tt.code, tt.reason)
		})
	}
}
