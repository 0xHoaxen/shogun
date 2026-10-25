package app

import (
	"bytes"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
)

const (
	// headerSearchRows is how many records at the top of a sheet may be looked
	// at for its header. Some exports, such as LinkedIn's, put notes first.
	headerSearchRows = 10
	// sourceSeparator joins two source columns that fill one text column, such
	// as First Name and Last Name into full_name.
	sourceSeparator = " "
)

// The orders a date with only numbers can be written in.
const (
	dateOrderYMD = "ymd"
	dateOrderDMY = "dmy"
	dateOrderMDY = "mdy"
)

// ColumnMapping fills one import column from columns of the uploaded sheet.
type ColumnMapping struct {
	Target  string
	Sources []string
}

// sheetShape is how a sheet with other columns maps onto the import columns.
type sheetShape struct {
	mappings []ColumnMapping
	// dateOrder is one of the dateOrder constants; empty reads only dates whose
	// order is unambiguous.
	dateOrder string
	// statuses maps a status as the sheet writes it, lower-cased, to ours.
	statuses map[string]domain.ContactStatus
}

// sheet is an uploaded CSV read into its header and the records below it.
type sheet struct {
	header  []string
	records [][]string
}

// readSheet reads any CSV and finds its header: the first of the top records
// with the most filled cells. Notes above the header are dropped.
func readSheet(data []byte) (sheet, error) {
	r := csv.NewReader(bytes.NewReader(bytes.TrimPrefix(data, utf8BOM)))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	r.LazyQuotes = true
	var records [][]string
	for {
		record, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return sheet{}, invalid("INVALID_CSV", "%v", err)
		}
		if len(records) > maxImportRows+headerSearchRows {
			return sheet{}, invalid("IMPORT_TOO_LARGE", "file has more than %d rows", maxImportRows)
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		return sheet{}, invalid("IMPORT_EMPTY", "file has no header row")
	}
	at := headerIndex(records)
	if len(records)-at-1 > maxImportRows {
		return sheet{}, invalid("IMPORT_TOO_LARGE", "file has more than %d rows", maxImportRows)
	}
	header := make([]string, len(records[at]))
	for i, name := range records[at] {
		header[i] = strings.TrimSpace(name)
	}
	return sheet{header: header, records: records[at+1:]}, nil
}

func headerIndex(records [][]string) int {
	best, bestFilled := 0, -1
	for i := 0; i < len(records) && i < headerSearchRows; i++ {
		if filled := filledCells(records[i]); filled > bestFilled {
			best, bestFilled = i, filled
		}
	}
	return best
}

func filledCells(record []string) int {
	n := 0
	for _, cell := range record {
		if strings.TrimSpace(cell) != "" {
			n++
		}
	}
	return n
}

// hasImportHeader reports whether the sheet already has the import columns.
func (sh sheet) hasImportHeader() bool {
	return checkImportHeader(sh.header) == nil
}

// shapeSheet writes the sheet as a CSV in the import columns, filling each
// from its mapped sources. Values the import would refuse, such as an unknown
// status or a date it cannot read, are left blank rather than failing the row.
func shapeSheet(sh sheet, shape sheetShape) ([]byte, error) {
	sourceIndex := make(map[string]int, len(sh.header))
	for i, name := range sh.header {
		if _, seen := sourceIndex[name]; !seen {
			sourceIndex[name] = i
		}
	}
	byTarget := make(map[string][]int, len(shape.mappings))
	for _, m := range shape.mappings {
		for _, source := range m.Sources {
			byTarget[m.Target] = append(byTarget[m.Target], sourceIndex[source])
		}
	}

	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if err := w.Write(importColumns); err != nil {
		return nil, fmt.Errorf("write header: %w", err)
	}
	for _, record := range sh.records {
		if filledCells(record) == 0 {
			continue
		}
		out := make([]string, len(importColumns))
		for i, target := range importColumns {
			out[i] = shapeCell(target, cellsAt(record, byTarget[target]), shape)
		}
		if err := w.Write(out); err != nil {
			return nil, fmt.Errorf("write row: %w", err)
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("write csv: %w", err)
	}
	return buf.Bytes(), nil
}

func cellsAt(record []string, indexes []int) []string {
	cells := make([]string, 0, len(indexes))
	for _, i := range indexes {
		if i < len(record) {
			if cell := strings.TrimSpace(record[i]); cell != "" {
				cells = append(cells, cell)
			}
		}
	}
	return cells
}

func shapeCell(target string, cells []string, shape sheetShape) string {
	switch target {
	case "tags":
		return strings.Join(cells, tagSeparator)
	case "last_contacted", "next_follow_up":
		if len(cells) == 0 {
			return ""
		}
		return normalizeDate(cells[0], shape.dateOrder)
	case "status":
		if len(cells) == 0 {
			return ""
		}
		return shapeStatus(cells[0], shape.statuses)
	default:
		return strings.Join(cells, sourceSeparator)
	}
}

func shapeStatus(cell string, statuses map[string]domain.ContactStatus) string {
	key := strings.ToLower(cell)
	if status, ok := statuses[key]; ok {
		return string(status)
	}
	if domain.ContactStatus(key).Valid() {
		return key
	}
	return ""
}

// Date layouts by the order their numbers are in. Layouts that name the month,
// or start with a four-digit year and a dash, are unambiguous and read under
// any order.
var (
	numericDateLayouts = map[string][]string{
		dateOrderYMD: {"2006/01/02", "2006/1/2", "2006.01.02", "2006.1.2"},
		dateOrderDMY: {"02/01/2006", "2/1/2006", "02-01-2006", "2-1-2006", "02.01.2006", "2.1.2006"},
		dateOrderMDY: {"01/02/2006", "1/2/2006", "01-02-2006", "1-2-2006"},
	}
	namedDateLayouts = []string{
		dateLayout, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02T15:04:05",
		"02 Jan 2006", "2 Jan 2006", "Jan 2, 2006", "January 2, 2006", "2 January 2006",
		"02-Jan-2006", "2-Jan-2006",
	}
)

// normalizeDate writes cell as YYYY-MM-DD, or returns "" if it is not a date
// it can read without guessing.
func normalizeDate(cell, order string) string {
	layouts := append(append([]string{}, namedDateLayouts...), numericDateLayouts[order]...)
	for _, layout := range layouts {
		if t, err := time.Parse(layout, cell); err == nil {
			return t.Format(dateLayout)
		}
	}
	return ""
}
