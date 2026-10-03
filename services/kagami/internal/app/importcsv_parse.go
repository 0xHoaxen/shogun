package app

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
)

const (
	// maxImportBytes stays under gRPC's default 4 MiB message limit.
	maxImportBytes = 3 << 20
	maxImportRows  = 5000

	tagSeparator = ";"
)

// utf8BOM is written at the start of CSV files by some spreadsheet exports.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// importColumns are the 17 columns of a contacts CSV, in order.
var importColumns = []string{
	"full_name", "company", "role", "email", "linkedin_url", "x_handle", "phone",
	"relationship", "how_we_met", "status", "preferred_channel", "last_contacted",
	"next_follow_up", "target_role", "job_link", "tags", "notes",
}

// importRow is one data row of a contacts CSV.
type importRow struct {
	// number is the 1-based CSV row number, counting the header as row 1.
	number  int32
	fields  ContactFields
	company string
	status  domain.ContactStatus
	jobLink string
	// problem is set when the row cannot be read; the row is then skipped.
	problem *InvalidArgumentError
}

// parseImportCSV reads a contacts CSV. File-level problems (size, header,
// quoting) are returned as an error; problems with one row are kept on the row
// so the other rows can still be reported on.
func parseImportCSV(data []byte) ([]importRow, error) {
	if len(data) > maxImportBytes {
		return nil, invalid("IMPORT_TOO_LARGE", "file is larger than %d bytes", maxImportBytes)
	}
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, utf8BOM)))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true

	header, err := r.Read()
	if errors.Is(err, io.EOF) {
		return nil, invalid("IMPORT_EMPTY", "file has no header row")
	}
	if err != nil {
		return nil, invalid("INVALID_CSV", "%v", err)
	}
	if err := checkImportHeader(header); err != nil {
		return nil, err
	}

	var rows []importRow
	for number := int32(2); ; number++ {
		record, err := r.Read()
		if errors.Is(err, io.EOF) {
			return rows, nil
		}
		if err != nil {
			return nil, invalid("INVALID_CSV", "%v", err)
		}
		if len(rows) == maxImportRows {
			return nil, invalid("IMPORT_TOO_LARGE", "file has more than %d rows", maxImportRows)
		}
		rows = append(rows, importRowFrom(number, record))
	}
}

func checkImportHeader(header []string) error {
	if len(header) == len(importColumns) {
		match := true
		for i, name := range header {
			match = match && strings.EqualFold(strings.TrimSpace(name), importColumns[i])
		}
		if match {
			return nil
		}
	}
	return invalid("INVALID_HEADER", "header must be exactly: %s", strings.Join(importColumns, ","))
}

func importRowFrom(number int32, record []string) importRow {
	row := importRow{number: number}
	if len(record) != len(importColumns) {
		row.problem = &InvalidArgumentError{
			Reason: "INVALID_ROW", Msg: fmt.Sprintf("expected %d columns, got %d", len(importColumns), len(record)),
		}
		return row
	}
	cell := func(column string) string {
		for i, name := range importColumns {
			if name == column {
				return strings.TrimSpace(record[i])
			}
		}
		return ""
	}
	row.fields = ContactFields{
		FullName: cell("full_name"), Role: cell("role"), Email: cell("email"),
		LinkedinURL: cell("linkedin_url"), XHandle: cell("x_handle"), Phone: cell("phone"),
		Relationship: cell("relationship"), HowWeMet: cell("how_we_met"),
		PreferredChannel: cell("preferred_channel"), LastContacted: cell("last_contacted"),
		NextFollowUp: cell("next_follow_up"), TargetRole: cell("target_role"), Notes: cell("notes"),
		Tags: splitTags(cell("tags")),
	}
	row.company = cell("company")
	row.jobLink = cell("job_link")
	if status := domain.ContactStatus(strings.ToLower(cell("status"))); status != "" {
		if !status.Valid() {
			row.problem = &InvalidArgumentError{Reason: "INVALID_STATUS", Field: "status", Msg: fmt.Sprintf("unknown status %q", status)}
		}
		row.status = status
	}
	return row
}

func splitTags(cell string) []string {
	if cell == "" {
		return nil
	}
	return strings.Split(cell, tagSeparator)
}
