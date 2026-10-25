package app

import (
	"strings"
	"testing"
	"unicode/utf8"

	"google.golang.org/protobuf/proto"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
)

// The response of a dry run holds the shaped sheet (at most 3 MiB, the import
// limit) and the preview rows. gRPC refuses a message over 4 MiB, so a preview
// whose every row is as large as a row can be, at the most rows it holds, must
// leave room for a full sheet.
func TestWorstCasePreviewFitsBesideAFullShapedSheet(t *testing.T) {
	const (
		grpcLimit   = 4 << 20
		importLimit = 3 << 20
	)
	cell := strings.Repeat("é", MaxPreviewCell) // two bytes a character
	row := &kagamiv1.ImportPreviewRow{
		Row: 99999, FullName: cell, Role: cell, Company: cell, Email: cell, Relationship: "recruiter",
		RelationshipGuessed: true, Duplicate: true, DuplicateContactId: strings.Repeat("0", 36),
		DuplicateReason: "name_company", RepeatsRow: 99999, Problem: cell,
	}
	rows := make([]*kagamiv1.ImportPreviewRow, MaxPreviewRows)
	for i := range rows {
		rows[i] = row
	}

	size := proto.Size(&kagamiv1.ImportContactsResponse{Rows: rows, RowsTruncated: true}) + importLimit

	if size >= grpcLimit {
		t.Fatalf("%d preview rows and a full sheet are %d bytes, over gRPC's %d: lower MaxPreviewRows or MaxPreviewCell", MaxPreviewRows, size, grpcLimit)
	}
	t.Logf("worst case %d bytes, %d to spare", size, grpcLimit-size)
}

func TestPreviewCellsAreClippedToAtLeastWhatARowNeedsOnScreen(t *testing.T) {
	long := strings.Repeat("x", 500)

	got := clip(long)

	if utf8.RuneCountInString(got) != MaxPreviewCell || !strings.HasSuffix(got, "…") {
		t.Fatalf("clipped to %d characters %q, want %d ending in an ellipsis", utf8.RuneCountInString(got), got, MaxPreviewCell)
	}
	if clip("Priya Raman") != "Priya Raman" {
		t.Error("a short cell must be left alone")
	}
	if got := clip(strings.Repeat("é", 200)); !utf8.ValidString(got) {
		t.Errorf("clipping split a character: %q", got)
	}
}
