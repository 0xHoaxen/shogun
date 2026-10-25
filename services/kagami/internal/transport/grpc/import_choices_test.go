package grpc_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/kagami/internal/app"
)

// failModel fails the test if the AI is called.
type failModel struct{ t *testing.T }

func (m failModel) Complete(context.Context, string, llm.Request) (llm.Response, error) {
	m.t.Error("the model was called although the user sent a mapping")
	return llm.Response{}, nil
}

func googleMapping() []*kagamiv1.ColumnMapping {
	return []*kagamiv1.ColumnMapping{
		{Target: "full_name", Sources: []string{"Given Name", "Family Name"}},
		{Target: "email", Sources: []string{"E-mail 1 - Value"}},
		{Target: "company", Sources: []string{"Organization Name"}},
	}
}

func (h *harness) importWith(t *testing.T, req *kagamiv1.ImportContactsRequest) (*kagamiv1.ImportContactsResponse, error) {
	t.Helper()
	return h.client.ImportContacts(h.ctx(t), req)
}

func TestAMappingTheUserSendsReplacesTheAIAndIsReturnedWithTheShapedSheet(t *testing.T) {
	h := newHarness(t, app.WithReshaper(failModel{t}))

	res, err := h.importWith(t, &kagamiv1.ImportContactsRequest{Csv: []byte(googleExport), DryRun: true, Reshape: true, Mappings: googleMapping()})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if res.GetReport().GetRowsCreated() != 2 || len(res.GetShapedCsv()) == 0 || len(res.GetMappings()) != 3 {
		t.Fatalf("res = %+v", res)
	}
	if res.GetRows()[0].GetFullName() != "Priya Raman" {
		t.Fatalf("preview row = %+v", res.GetRows()[0])
	}
}

func TestABadMappingIsRefusedInTheUsersOwnWordsAndWritesNothing(t *testing.T) {
	tests := []struct {
		name   string
		maps   []*kagamiv1.ColumnMapping
		code   codes.Code
		reason string
	}{
		{"unknown target", []*kagamiv1.ColumnMapping{{Target: "full_name", Sources: []string{"Given Name"}}, {Target: "shoe_size", Sources: []string{"Organization Name"}}}, codes.InvalidArgument, "INVALID_MAPPING"},
		{"repeated target", []*kagamiv1.ColumnMapping{{Target: "full_name", Sources: []string{"Given Name"}}, {Target: "full_name", Sources: []string{"Family Name"}}}, codes.InvalidArgument, "INVALID_MAPPING"},
		{"unknown source", []*kagamiv1.ColumnMapping{{Target: "full_name", Sources: []string{"Nope"}}}, codes.InvalidArgument, "INVALID_MAPPING"},
		{"source used twice", []*kagamiv1.ColumnMapping{{Target: "full_name", Sources: []string{"Given Name"}}, {Target: "company", Sources: []string{"Given Name"}}}, codes.InvalidArgument, "INVALID_MAPPING"},
		{"no name", []*kagamiv1.ColumnMapping{{Target: "email", Sources: []string{"E-mail 1 - Value"}}}, codes.FailedPrecondition, "IMPORT_NO_NAME_COLUMN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)

			_, err := h.importWith(t, &kagamiv1.ImportContactsRequest{Csv: []byte(googleExport), Mappings: tt.maps})

			requireStatus(t, err, tt.code, tt.reason)
			if strings.Contains(strings.ToLower(err.Error()), "ai provider") {
				t.Errorf("a user's mistake blamed the AI: %v", err)
			}
			if n := h.rowCount(t, "contacts"); n != 0 {
				t.Errorf("%d contacts written", n)
			}
		})
	}
}

const ambiguousDates = "Name,Last talked\nAsha Rao,03/04/2026\n"

func TestDateOrderDecidesHowNumericDatesAreRead(t *testing.T) {
	maps := []*kagamiv1.ColumnMapping{{Target: "full_name", Sources: []string{"Name"}}, {Target: "last_contacted", Sources: []string{"Last talked"}}}
	for order, want := range map[string]string{"dmy": "2026-04-03", "mdy": "2026-03-04", "": ""} {
		h := newHarness(t)
		if _, err := h.importWith(t, &kagamiv1.ImportContactsRequest{Csv: []byte(ambiguousDates), Mappings: maps, DateOrder: order}); err != nil {
			t.Fatalf("import with order %q: %v", order, err)
		}
		list, err := h.client.ListContacts(h.ctx(t), &kagamiv1.ListContactsRequest{})
		if err != nil || len(list.GetContacts()) != 1 || list.GetContacts()[0].GetLastContacted() != want {
			t.Errorf("order %q: %v, %v; want %q", order, list.GetContacts(), err, want)
		}
	}
	h := newHarness(t)
	_, err := h.importWith(t, &kagamiv1.ImportContactsRequest{Csv: []byte(ambiguousDates), Mappings: maps, DateOrder: "banana"})
	requireStatus(t, err, codes.InvalidArgument, "INVALID_MAPPING")
}

func TestTheAIsDateOrderIsReturnedToSendBack(t *testing.T) {
	answer := `{"columns":[{"target":"full_name","sources":["Name"]},{"target":"last_contacted","sources":["Last talked"]}],"date_order":"dmy"}`
	h := newHarness(t, app.WithReshaper(stubModel{text: answer}))

	res, err := h.importWith(t, &kagamiv1.ImportContactsRequest{Csv: []byte(ambiguousDates), DryRun: true, Reshape: true})

	if err != nil || res.GetDateOrder() != "dmy" {
		t.Fatalf("date order %q, err %v", res.GetDateOrder(), err)
	}
}

func sel(rows ...int32) *kagamiv1.RowSelection { return &kagamiv1.RowSelection{Rows: rows} }

func TestOnlySelectedRowsAreImportedAndDuplicatesNeedTicking(t *testing.T) {
	h := newHarness(t)
	h.addContact(t, "Priya Raman", "priya@lumen.example", "")
	csv := sheet(
		[6]string{"Priya R", "Lumen", "", "priya@lumen.example", "", ""},        // row 2: duplicate
		[6]string{"Tom Becker", "Corvid", "", "tom@corvid.example", "", ""},     // row 3
		[6]string{"Ana Ruiz", "Northwind", "", "ana@northwind.example", "", ""}, // row 4
	)

	res, err := h.importWith(t, &kagamiv1.ImportContactsRequest{Csv: []byte(csv), Selection: sel(3)})

	r := res.GetReport()
	if err != nil || r.GetRowsTotal() != 1 || r.GetRowsCreated() != 1 || r.GetRowsUpdated() != 0 || r.GetRowsSkipped() != 2 {
		t.Fatalf("report = %+v, err %v", r, err)
	}
	if n := h.rowCount(t, "contacts"); n != 2 {
		t.Fatalf("%d contacts, want Priya and Tom only", n)
	}

	// Ticking the duplicate updates that person and adds nobody.
	res, err = h.importWith(t, &kagamiv1.ImportContactsRequest{Csv: []byte(csv), Selection: sel(2)})
	if err != nil || res.GetReport().GetRowsUpdated() != 1 || res.GetReport().GetRowsCreated() != 0 || h.rowCount(t, "contacts") != 2 {
		t.Fatalf("report = %+v, err %v", res.GetReport(), err)
	}
	if n := h.outboxCount(t, "contact.added"); n != 2 { // Priya's own add and Tom's
		t.Fatalf("%d contact.added, want 2", n)
	}
}

func TestASelectionOfRowsTheFileDoesNotHaveIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	h := newHarness(t)
	csv := sheet([6]string{"Tom Becker", "Corvid", "", "tom@corvid.example", "", ""})
	for _, bad := range []int32{0, 1, 3, -4, 99} {
		_, err := h.importWith(t, &kagamiv1.ImportContactsRequest{Csv: []byte(csv), Selection: sel(2, bad)})
		requireStatus(t, err, codes.InvalidArgument, "INVALID_ROW_SELECTION")
	}
	if n := h.rowCount(t, "contacts"); n != 0 {
		t.Fatalf("%d contacts written", n)
	}
}

func TestAnEmptySelectionImportsNothingAndRepeatsAreFine(t *testing.T) {
	h := newHarness(t)
	csv := sheet([6]string{"Tom Becker", "Corvid", "", "tom@corvid.example", "", ""}, [6]string{"Ana Ruiz", "N", "", "ana@n.example", "", ""})

	none, err := h.importWith(t, &kagamiv1.ImportContactsRequest{Csv: []byte(csv), Selection: sel()})
	if err != nil || none.GetReport().GetRowsTotal() != 0 || none.GetReport().GetRowsSkipped() != 2 || h.rowCount(t, "contacts") != 0 {
		t.Fatalf("empty selection: %+v, %v", none.GetReport(), err)
	}
	dup, err := h.importWith(t, &kagamiv1.ImportContactsRequest{Csv: []byte(csv), Selection: sel(3, 3, 3)})
	if err != nil || dup.GetReport().GetRowsCreated() != 1 || h.rowCount(t, "contacts") != 1 {
		t.Fatalf("repeated row: %+v, %v", dup.GetReport(), err)
	}
}

func TestADryRunWithASelectionCountsOnlyTheSelectionButPreviewsEveryRow(t *testing.T) {
	h := newHarness(t)
	csv := sheet([6]string{"Tom Becker", "Corvid", "", "tom@corvid.example", "", ""}, [6]string{"Ana Ruiz", "N", "", "ana@n.example", "", ""})

	res, err := h.importWith(t, &kagamiv1.ImportContactsRequest{Csv: []byte(csv), DryRun: true, Selection: sel(2)})

	if err != nil || res.GetReport().GetRowsCreated() != 1 || res.GetReport().GetRowsSkipped() != 1 || len(res.GetRows()) != 2 || h.rowCount(t, "contacts") != 0 {
		t.Fatalf("res = %+v, err %v", res, err)
	}
}

func TestAnUnreadableRowIsReportedOnlyIfSelected(t *testing.T) {
	h := newHarness(t)

	skipped, _ := h.importWith(t, &kagamiv1.ImportContactsRequest{Csv: []byte(goodAndBadRows), Selection: sel(2, 3)})
	picked, _ := h.importWith(t, &kagamiv1.ImportContactsRequest{Csv: []byte(goodAndBadRows), DryRun: true, Selection: sel(4)})

	if skipped.GetReport().GetRowsFailed() != 0 || skipped.GetReport().GetRowsCreated() != 2 {
		t.Errorf("unselected bad rows were reported: %+v", skipped.GetReport())
	}
	if picked.GetReport().GetRowsFailed() != 1 {
		t.Errorf("a selected bad row was not reported: %+v", picked.GetReport())
	}
}

func TestTheFiveThousandRowLimitHoldsOnEveryPath(t *testing.T) {
	rows := func(n int, header string, line func(i int) string) []byte {
		var b strings.Builder
		b.WriteString(header)
		for i := 0; i < n; i++ {
			b.WriteString(line(i))
		}
		return []byte(b.String())
	}
	direct := func(n int) []byte {
		return rows(n, csvHeader, func(i int) string { return fmt.Sprintf("P%d,,,,,,,,,,,,,,,,\n", i) })
	}
	foreign := func(n int) []byte {
		return rows(n, "Name\n", func(i int) string { return fmt.Sprintf("P%d\n", i) })
	}
	maps := []*kagamiv1.ColumnMapping{{Target: "full_name", Sources: []string{"Name"}}}
	answer := `{"columns":[{"target":"full_name","sources":["Name"]}]}`
	for name, tt := range map[string]struct {
		opts []app.Option
		req  func(n int) *kagamiv1.ImportContactsRequest
	}{
		"direct": {nil, func(n int) *kagamiv1.ImportContactsRequest {
			return &kagamiv1.ImportContactsRequest{Csv: direct(n), DryRun: true}
		}},
		"reshaped": {[]app.Option{app.WithReshaper(stubModel{text: answer})}, func(n int) *kagamiv1.ImportContactsRequest {
			return &kagamiv1.ImportContactsRequest{Csv: foreign(n), DryRun: true, Reshape: true}
		}},
		"user mapped": {nil, func(n int) *kagamiv1.ImportContactsRequest {
			return &kagamiv1.ImportContactsRequest{Csv: foreign(n), DryRun: true, Mappings: maps}
		}},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t, tt.opts...)

			ok, err := h.importWith(t, tt.req(5000))
			if err != nil || ok.GetReport().GetRowsTotal() != 5000 {
				t.Fatalf("5000 rows: %+v, %v", ok.GetReport(), err)
			}
			_, err = h.importWith(t, tt.req(5001))
			requireStatus(t, err, codes.InvalidArgument, "IMPORT_TOO_LARGE")
		})
	}
}
