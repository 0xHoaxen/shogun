package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/0xHoaxen/shogun/pkg/llm"
)

// fakeCompleter answers every call with text or err and records the requests.
type fakeCompleter struct {
	text  string
	err   error
	calls []llm.Request
}

func (f *fakeCompleter) Complete(_ context.Context, feature string, req llm.Request) (llm.Response, error) {
	if feature != featureImportReshape {
		return llm.Response{}, fmt.Errorf("unexpected feature %q", feature)
	}
	f.calls = append(f.calls, req)
	return llm.Response{Text: f.text}, f.err
}

// linkedInExport is shaped like LinkedIn's Connections.csv: notes, a blank
// line, then the header.
const linkedInExport = "Notes:\n" +
	"\"When exporting your connection data, you may notice that some of the email addresses are missing.\"\n" +
	"\n" +
	"First Name,Last Name,URL,Email Address,Company,Position,Connected On,Stage\n" +
	"Asha,Nair,https://www.linkedin.com/in/asha-nair,asha@example.com,Lumen,Staff Engineer,02 Jan 2024,Contacted\n" +
	"Ravi,Iyer,https://www.linkedin.com/in/ravi-iyer,,Northwind,Recruiter,15/03/2024,Unknown stage\n" +
	",,,,,,,\n"

const linkedInAnswer = `{"columns":[
 {"target":"full_name","sources":["First Name","Last Name"]},
 {"target":"linkedin_url","sources":["URL"]},
 {"target":"email","sources":["Email Address"]},
 {"target":"company","sources":["Company"]},
 {"target":"role","sources":["Position"]},
 {"target":"last_contacted","sources":["Connected On"]},
 {"target":"status","sources":["Stage"]}],
 "date_order":"dmy","status_values":{"Contacted":"reached_out"}}`

func TestReshapeImportCSVMapsAnExportOntoTheImportColumns(t *testing.T) {
	// Arrange
	model := &fakeCompleter{text: "Here you go:\n```json\n" + linkedInAnswer + "\n```"}
	svc := NewService(nil, nil, WithReshaper(model))

	// Act
	shaped, mappings, err := svc.reshapeImportCSV(context.Background(), []byte(linkedInExport))
	// Assert
	if err != nil {
		t.Fatalf("reshape: %v", err)
	}
	if len(mappings) != 7 || mappings[0].Target != "full_name" || len(mappings[0].Sources) != 2 {
		t.Fatalf("mappings = %+v", mappings)
	}
	rows, err := parseImportCSV(shaped)
	if err != nil {
		t.Fatalf("shaped CSV does not import: %v\n%s", err, shaped)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2 (the blank record is dropped)", len(rows))
	}
	asha, ravi := rows[0], rows[1]
	if asha.fields.FullName != "Asha Nair" || asha.fields.Email != "asha@example.com" ||
		asha.company != "Lumen" || asha.fields.Role != "Staff Engineer" ||
		asha.fields.LastContacted != "2024-01-02" || asha.status != "reached_out" || asha.problem != nil {
		t.Errorf("asha = %+v", asha)
	}
	if ravi.fields.LastContacted != "2024-03-15" || ravi.status != "" || ravi.problem != nil {
		t.Errorf("ravi = %+v, want dmy date and an unknown stage left blank", ravi)
	}
}

func TestReshapeImportCSVShowsTheModelOnlyASample(t *testing.T) {
	// Arrange
	long := strings.Repeat("é", reshapeCellRunes+20)
	var sheet strings.Builder
	sheet.WriteString("Name,Bio\n")
	for i := range reshapeSampleRows + 3 {
		fmt.Fprintf(&sheet, "Person %d,%s\n", i, long)
	}
	model := &fakeCompleter{text: `{"columns":[{"target":"full_name","sources":["Name"]}]}`}
	svc := NewService(nil, nil, WithReshaper(model))

	// Act
	shaped, _, err := svc.reshapeImportCSV(context.Background(), []byte(sheet.String()))
	// Assert
	if err != nil {
		t.Fatalf("reshape: %v", err)
	}
	if len(model.calls) != 1 {
		t.Fatalf("calls = %d, want 1", len(model.calls))
	}
	var sent struct {
		Header []string   `json:"header"`
		Rows   [][]string `json:"rows"`
	}
	if err := json.Unmarshal([]byte(model.calls[0].Messages[0].Content), &sent); err != nil {
		t.Fatalf("prompt is not JSON: %v", err)
	}
	if len(sent.Rows) != reshapeSampleRows {
		t.Errorf("sample rows = %d, want %d", len(sent.Rows), reshapeSampleRows)
	}
	if got := len([]rune(sent.Rows[0][1])); got != reshapeCellRunes {
		t.Errorf("sample cell = %d runes, want %d", got, reshapeCellRunes)
	}
	rows, err := parseImportCSV(shaped)
	if err != nil || len(rows) != reshapeSampleRows+3 {
		t.Errorf("every row is shaped, not only the sample: rows = %d, err = %v", len(rows), err)
	}
}

func TestReshapeImportCSVLeavesAnImportSheetAlone(t *testing.T) {
	// Arrange
	model := &fakeCompleter{}
	svc := NewService(nil, nil, WithReshaper(model))
	data := importHeader + "Asha Nair,,,,,,,,,,,,,,,,\n"

	// Act
	shaped, mappings, err := svc.reshapeImportCSV(context.Background(), []byte(data))

	// Assert
	if err != nil || shaped != nil || mappings != nil {
		t.Fatalf("got %q, %v, %v; want nothing", shaped, mappings, err)
	}
	if len(model.calls) != 0 {
		t.Errorf("model was called %d times for a sheet already in shape", len(model.calls))
	}
}

func TestReshapeImportCSVWithoutAModel(t *testing.T) {
	svc := NewService(nil, nil)
	_, _, err := svc.reshapeImportCSV(context.Background(), []byte("Name\nAsha\n"))
	if !errors.Is(err, ErrReshapeOff) {
		t.Fatalf("err = %v, want ErrReshapeOff", err)
	}
}

func TestReshapeImportCSVRefusesABadAnswer(t *testing.T) {
	const sheet = "First Name,Email\nAsha,asha@example.com\n"
	tests := []struct {
		name, answer, reason string
	}{
		{"not json", "I could not find a mapping.", reasonReshapeFailed},
		{"unknown target", `{"columns":[{"target":"full_name","sources":["First Name"]},{"target":"salary","sources":["Email"]}]}`, reasonReshapeFailed},
		{"repeated target", `{"columns":[{"target":"full_name","sources":["First Name"]},{"target":"full_name","sources":["Email"]}]}`, reasonReshapeFailed},
		{"invented source", `{"columns":[{"target":"full_name","sources":["Full Name"]}]}`, reasonReshapeFailed},
		{"source used twice", `{"columns":[{"target":"full_name","sources":["First Name"]},{"target":"notes","sources":["First Name"]}]}`, reasonReshapeFailed},
		{"no name", `{"columns":[{"target":"email","sources":["Email"]}]}`, reasonNoNameColumn},
		{"nothing mapped", `{"columns":[]}`, reasonNoNameColumn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(nil, nil, WithReshaper(&fakeCompleter{text: tt.answer}))
			_, _, err := svc.reshapeImportCSV(context.Background(), []byte(sheet))
			var re *ReshapeError
			if !errors.As(err, &re) || re.Reason != tt.reason {
				t.Fatalf("err = %v, want a ReshapeError %s", err, tt.reason)
			}
		})
	}
}

func TestReshapeImportCSVReportsModelFailures(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		keep   error
		reason string
	}{
		{"no provider", llm.ErrProviderNotConfigured, llm.ErrProviderNotConfigured, ""},
		{"budget spent", fmt.Errorf("reserve: %w", llm.ErrBudgetExhausted), llm.ErrBudgetExhausted, ""},
		{"no metering", llm.ErrMeteringUnavailable, llm.ErrMeteringUnavailable, ""},
		{"refused", llm.ErrRefused, nil, reasonReshapeFailed},
		{"provider down", &llm.StatusError{StatusCode: 503}, nil, reasonReshapeFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(nil, nil, WithReshaper(&fakeCompleter{err: tt.err}))
			_, _, err := svc.reshapeImportCSV(context.Background(), []byte("Name\nAsha\n"))
			if tt.keep != nil {
				if !errors.Is(err, tt.keep) {
					t.Fatalf("err = %v, want it to wrap %v", err, tt.keep)
				}
				return
			}
			var re *ReshapeError
			if !errors.As(err, &re) || re.Reason != tt.reason {
				t.Fatalf("err = %v, want a ReshapeError %s", err, tt.reason)
			}
		})
	}
}

func TestNormalizeDate(t *testing.T) {
	tests := []struct {
		cell, order, want string
	}{
		{"2024-03-15", "", "2024-03-15"},
		{"02 Jan 2024", "", "2024-01-02"},
		{"Jan 2, 2024", "mdy", "2024-01-02"},
		{"2024-03-15T10:00:00Z", "", "2024-03-15"},
		{"15/03/2024", "dmy", "2024-03-15"},
		{"3/15/2024", "mdy", "2024-03-15"},
		{"2024/3/15", "ymd", "2024-03-15"},
		{"03/04/2024", "", ""},
		{"15/03/2024", "mdy", ""},
		{"last week", "dmy", ""},
	}
	for _, tt := range tests {
		if got := normalizeDate(tt.cell, tt.order); got != tt.want {
			t.Errorf("normalizeDate(%q, %q) = %q, want %q", tt.cell, tt.order, got, tt.want)
		}
	}
}

func TestReadSheetFindsTheHeaderBelowNotes(t *testing.T) {
	sh, err := readSheet([]byte(linkedInExport))
	if err != nil {
		t.Fatalf("readSheet: %v", err)
	}
	if sh.header[0] != "First Name" || len(sh.records) != 3 {
		t.Errorf("header = %v, records = %d", sh.header, len(sh.records))
	}
}
