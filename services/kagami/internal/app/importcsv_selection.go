package app

import "slices"

// Reason of the InvalidArgumentError for a row the user picked that the file
// does not have.
const reasonInvalidRowSelection = "INVALID_ROW_SELECTION"

// RowSelection is the rows of a file to import. Row numbers are the 1-based CSV
// rows of the file that is applied, counting the header as row 1: the numbers a
// dry run's preview gives. They only mean something for the same file and
// column mapping, so a screen that changes either previews again first.
type RowSelection struct {
	Rows []int32
}

// selectRows keeps the rows picked in sel, in file order, and says how many were
// left out. A nil selection keeps every row. A number that is not a row of the
// file is refused before anything is applied, so a stale preview cannot import the
// wrong people.
func selectRows(rows []importRow, sel *RowSelection) (kept []importRow, skipped int32, err error) {
	if sel == nil {
		return rows, 0, nil
	}
	picked := make(map[int32]bool, len(sel.Rows))
	for _, n := range sel.Rows {
		picked[n] = true
	}
	known := make(map[int32]bool, len(rows))
	for _, row := range rows {
		known[row.number] = true
	}
	for _, n := range sortedKeys(picked) {
		if !known[n] {
			return nil, 0, invalid(reasonInvalidRowSelection, "row %d is not a row of this file", n)
		}
	}
	kept = make([]importRow, 0, len(picked))
	for _, row := range rows {
		if picked[row.number] {
			kept = append(kept, row)
		}
	}
	return kept, int32(len(rows) - len(kept)), nil
}

func sortedKeys(m map[int32]bool) []int32 {
	keys := make([]int32, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
