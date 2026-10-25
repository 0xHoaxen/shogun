package grpc_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/services/kagami/internal/app"
)

// sheet builds a contacts CSV from rows of name, company, role, email, linkedin, relationship.
func sheet(rows ...[6]string) string {
	var b strings.Builder
	b.WriteString(csvHeader)
	for _, r := range rows {
		fmt.Fprintf(&b, "%s,%s,%s,%s,%s,,,%s,,,,,,,,,\n", r[0], r[1], r[2], r[3], r[4], r[5])
	}
	return b.String()
}

func (h *harness) preview(t *testing.T, csv string) []*kagamiv1.ImportPreviewRow {
	t.Helper()
	return h.importCSV(t, csv, true).GetRows()
}

func TestDryRunPreviewsEveryRowWithAGuessedRelationship(t *testing.T) {
	// Arrange
	h := newHarness(t)

	// Act
	rows := h.preview(t, sheet(
		[6]string{"Sana Iyer", "Lumen", "Technical Recruiter", "sana@lumen.example", "", ""},
		[6]string{"Tom Becker", "Corvid", "Engineering Manager", "tom@corvid.example", "", ""},
		[6]string{"Ana Ruiz", "Northwind", "Backend Engineer", "ana@northwind.example", "", ""},
		[6]string{"Lee Park", "Northwind", "Designer", "lee@northwind.example", "", ""},
		[6]string{"Pat Doe", "Northwind", "", "pat@northwind.example", "", ""},
		[6]string{"Kim Soo", "Northwind", "Technical Recruiter", "kim@northwind.example", "", "friend"},
	))

	// Assert
	want := []struct {
		name, rel string
		guessed   bool
	}{
		{"Sana Iyer", "recruiter", true},
		{"Tom Becker", "manager", true},
		{"Ana Ruiz", "engineer", true},
		{"Lee Park", "other", true},
		{"Pat Doe", "other", false},
		{"Kim Soo", "friend", false},
	}
	if len(rows) != len(want) {
		t.Fatalf("%d preview rows, want %d", len(rows), len(want))
	}
	for i, w := range want {
		r := rows[i]
		if r.GetRow() != int32(i+2) || r.GetFullName() != w.name || r.GetRelationship() != w.rel || r.GetRelationshipGuessed() != w.guessed {
			t.Errorf("row %d = %+v, want %s as %s (guessed %v) on CSV row %d", i, r, w.name, w.rel, w.guessed, i+2)
		}
	}
	if rows[0].GetCompany() != "Lumen" || rows[0].GetRole() != "Technical Recruiter" || rows[0].GetEmail() != "sana@lumen.example" {
		t.Errorf("row 0 = %+v, want the cells the file held", rows[0])
	}
}

func TestPreviewFlagsPeopleAlreadyInTheBook(t *testing.T) {
	h := newHarness(t)
	byEmail := h.addContact(t, "Priya Raman", "priya@lumen.example", "")
	withLinkedin, err := h.client.AddContact(h.ctx(t), &kagamiv1.AddContactRequest{
		Contact: &kagamiv1.Contact{FullName: "Arjun Mehta", LinkedinUrl: "https://linkedin.com/in/arjun"}, CompanyName: "Corvid",
	})
	if err != nil {
		t.Fatalf("add contact: %v", err)
	}
	byName, err := h.client.AddContact(h.ctx(t), &kagamiv1.AddContactRequest{
		Contact: &kagamiv1.Contact{FullName: "Sana Iyer", Email: "sana.iyer@old.example"}, CompanyName: "Tessellate",
	})
	if err != nil {
		t.Fatalf("add contact: %v", err)
	}

	rows := h.preview(t, sheet(
		[6]string{"P. Raman", "Elsewhere", "", "PRIYA@Lumen.Example", "", ""},       // email, any case
		[6]string{"A. Mehta", "Other", "", "", "https://linkedin.com/in/arjun", ""}, // LinkedIn
		[6]string{"  sana   IYER ", "tessellate", "", "sana@new.example", "", ""},   // name + company, spacing and case
		[6]string{"Sana Iyer", "Lumen", "", "sana@lumen.example", "", ""},           // same name, another company
		[6]string{"Sana Iyer", "", "", "sana2@lumen.example", "", ""},               // same name, no company in the file
		[6]string{"Brand New", "Lumen", "", "new@lumen.example", "", ""},            // nobody
	))

	type dup struct {
		is     bool
		id     string
		reason string
	}
	want := []dup{
		{true, byEmail.GetId(), "email"},
		{true, withLinkedin.GetContact().GetId(), "linkedin"},
		{true, byName.GetContact().GetId(), "name_company"},
		{},
		{},
		{},
	}
	for i, w := range want {
		r := rows[i]
		if r.GetDuplicate() != w.is || r.GetDuplicateContactId() != w.id || r.GetDuplicateReason() != w.reason {
			t.Errorf("row %d (%s) = duplicate %v id %q reason %q, want %+v", i, r.GetFullName(), r.GetDuplicate(), r.GetDuplicateContactId(), r.GetDuplicateReason(), w)
		}
	}
}

func TestPreviewNeverMatchesAnotherOwnersOrAnArchivedContact(t *testing.T) {
	h := newHarness(t)
	if _, err := h.client.AddContact(h.ctxFor(t, otherOwner), &kagamiv1.AddContactRequest{
		Contact: &kagamiv1.Contact{FullName: "Priya Raman", Email: "priya@lumen.example"}, CompanyName: "Lumen",
	}); err != nil {
		t.Fatalf("add contact: %v", err)
	}
	mine := h.addContact(t, "Arjun Mehta", "arjun@corvid.example", "")
	if _, err := h.pool.Exec(context.Background(), `UPDATE contacts SET archived_at = now() WHERE id = $1`, mine.GetId()); err != nil {
		t.Fatalf("archive: %v", err)
	}

	rows := h.preview(t, sheet(
		[6]string{"Priya Raman", "Lumen", "", "priya@lumen.example", "", ""},
		[6]string{"Arjun Mehta", "Lumen", "", "", "", ""},
	))

	for _, r := range rows {
		if r.GetDuplicate() && r.GetDuplicateReason() != "email" {
			t.Errorf("%s matched by %s, want a person in someone else's book to be invisible", r.GetFullName(), r.GetDuplicateReason())
		}
	}
	if rows[0].GetDuplicate() {
		t.Error("another owner's contact was reported as a duplicate")
	}
	// Archived contacts are still found by email (the import would update them), but not by name.
	if rows[1].GetDuplicate() {
		t.Error("an archived contact was matched by name")
	}
}

func TestPreviewSaysWhichEarlierRowARepeatedPersonIs(t *testing.T) {
	h := newHarness(t)

	rows := h.preview(t, sheet(
		[6]string{"Sana Iyer", "Lumen", "", "sana@lumen.example", "", ""},        // row 2
		[6]string{"Tom Becker", "Corvid", "", "tom@corvid.example", "", ""},      // row 3
		[6]string{"S. Iyer", "Other", "", "SANA@lumen.example", "", ""},          // row 4: same email
		[6]string{"Tom  BECKER", "corvid", "", "tom2@corvid.example", "", ""},    // row 5: same name and company
		[6]string{"Dee Lo", "Lumen", "", "", "https://linkedin.com/in/dee", ""},  // row 6
		[6]string{"Dee Low", "Lumen", "", "", "https://linkedin.com/in/dee", ""}, // row 7: same LinkedIn
		[6]string{"Sana Iyer", "Lumen", "", "sana3@lumen.example", "", ""},       // row 8: same name and company as row 2
	))

	var got []int32
	for _, r := range rows {
		got = append(got, r.GetRepeatsRow())
	}
	want := []int32{0, 0, 2, 3, 0, 6, 2}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("repeats_row = %v, want %v", got, want)
		}
	}
}

func TestPreviewKeepsRowsThatCannotBeImportedWithTheirProblem(t *testing.T) {
	h := newHarness(t)

	rows := h.preview(t, goodAndBadRows)

	if len(rows) != 4 {
		t.Fatalf("%d preview rows, want all 4 including the failing ones", len(rows))
	}
	if rows[0].GetProblem() != "" || rows[1].GetProblem() != "" {
		t.Errorf("good rows have problems: %q, %q", rows[0].GetProblem(), rows[1].GetProblem())
	}
	if rows[2].GetProblem() == "" || rows[3].GetProblem() == "" {
		t.Errorf("failing rows have no problem: %q, %q", rows[2].GetProblem(), rows[3].GetProblem())
	}
	if rows[3].GetRelationship() != "" || rows[3].GetDuplicate() {
		t.Errorf("a row that cannot be read should carry no guess: %+v", rows[3])
	}
	// A typed relationship that is not one of the six is the row's problem, not a guess.
	bad := h.preview(t, sheet([6]string{"Ana Ruiz", "Northwind", "Backend Engineer", "ana@northwind.example", "", "colleague"}))
	if bad[0].GetProblem() == "" || bad[0].GetRelationshipGuessed() {
		t.Errorf("a made-up relationship should be a problem: %+v", bad[0])
	}
}

func TestDryRunPreviewWritesNothingAndAFullImportReturnsNoPreview(t *testing.T) {
	h := newHarness(t)
	csv := sheet([6]string{"Sana Iyer", "Lumen", "Recruiter", "sana@lumen.example", "", ""})

	dry := h.importCSV(t, csv, true)

	if len(dry.GetRows()) != 1 || dry.GetReport().GetRowsCreated() != 1 {
		t.Fatalf("dry run = %+v", dry)
	}
	for _, table := range []string{"contacts", "companies", "imports", "outbox"} {
		if n := h.rowCount(t, table); n != 0 {
			t.Errorf("%s has %d rows after a dry run, want 0: the preview must not create a company", table, n)
		}
	}
	applied := h.importCSV(t, csv, false)
	if len(applied.GetRows()) != 0 || applied.GetReport().GetRowsCreated() != 1 {
		t.Fatalf("real import = %+v, want the counts and no preview rows", applied)
	}
	again := h.importCSV(t, csv, true)
	if !again.GetRows()[0].GetDuplicate() || again.GetRows()[0].GetDuplicateReason() != "email" {
		t.Fatalf("after importing, the same sheet previews as a duplicate: %+v", again.GetRows()[0])
	}
}

func TestPreviewOfAFullSizeSheetHoldsTheFirstRowsAndSaysItWasCut(t *testing.T) {
	h := newHarness(t)
	var b strings.Builder
	b.WriteString(csvHeader)
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&b, "Person Number %d,Company %d,Senior Staff Platform Engineer,person%d@example-company-%d.example,https://linkedin.com/in/person-%d,,,,,,,,,,,,\n", i, i%50, i, i%50, i)
	}

	res := h.importCSV(t, b.String(), true)

	if len(res.GetRows()) != 1000 || !res.GetRowsTruncated() || res.GetRows()[999].GetRow() != 1001 {
		t.Fatalf("%d preview rows, truncated %v; want the first 1000 and the flag", len(res.GetRows()), res.GetRowsTruncated())
	}
	// The counts still cover every row, so nothing about the import is cut.
	if res.GetReport().GetRowsTotal() != 5000 || res.GetReport().GetRowsCreated() != 5000 {
		t.Fatalf("report = %+v, want all 5000 rows counted", res.GetReport())
	}
}

// The response of a reshaped dry run carries the whole shaped sheet and a
// preview row for every row. gRPC refuses a message over 4 MiB, and the import
// already keeps the file under 3 MiB for that reason, so the worst case has to fit.
func TestReshapedDryRunOfAWorstCaseSheetFitsInOneGRPCMessage(t *testing.T) {
	const grpcDefaultLimit = 4 << 20
	h := newHarness(t, app.WithReshaper(stubModel{text: googleAnswer}))
	// Each source row becomes a shaped row about 13 commas longer, so about 2.7 MiB
	// of source is as near the 3 MiB cap on the shaped sheet as this gets.
	var b strings.Builder
	b.WriteString("Given Name,Family Name,E-mail 1 - Value,Organization Name\n")
	for i := 0; b.Len() < 2_700_000 && i < 5000; i++ {
		fmt.Fprintf(&b, "Given%04d %s,Family%04d %s,person%04d@%s.example,Company%04d %s\n",
			i, strings.Repeat("g", 200), i, strings.Repeat("f", 200), i, strings.Repeat("d", 60), i%50, strings.Repeat("c", 100))
	}

	res, err := h.client.ImportContacts(h.ctx(t), &kagamiv1.ImportContactsRequest{Csv: []byte(b.String()), DryRun: true, Reshape: true})
	if err != nil {
		t.Fatalf("import: %v", err)
	}

	size := proto.Size(res)
	if !res.GetRowsTruncated() && len(res.GetRows()) > 1000 {
		t.Fatalf("%d preview rows without the truncated flag", len(res.GetRows()))
	}
	t.Logf("%d preview rows, shaped sheet %d bytes, whole response %d bytes", len(res.GetRows()), len(res.GetShapedCsv()), size)
	if size >= grpcDefaultLimit {
		t.Fatalf("response is %d bytes, which gRPC would refuse (limit %d)", size, grpcDefaultLimit)
	}
}

func TestAShortFileIsNotMarkedAsCut(t *testing.T) {
	h := newHarness(t)

	res := h.importCSV(t, sheet([6]string{"Sana Iyer", "Lumen", "Recruiter", "sana@lumen.example", "", ""}), true)

	if res.GetRowsTruncated() || len(res.GetRows()) != 1 {
		t.Fatalf("truncated %v with %d rows, want a whole preview", res.GetRowsTruncated(), len(res.GetRows()))
	}
}

func TestPreviewCellsAreShortenedButTheImportKeepsThemWhole(t *testing.T) {
	h := newHarness(t)
	longName := "Dr. " + strings.Repeat("Longname ", 20)
	csv := sheet([6]string{longName, "Lumen", "Recruiter", "long@lumen.example", "", ""})

	res := h.importCSV(t, csv, true)
	h.importCSV(t, csv, false)

	if got := res.GetRows()[0].GetFullName(); len([]rune(got)) != 60 || !strings.HasSuffix(got, "…") {
		t.Fatalf("preview name %q (%d characters), want 60 ending in an ellipsis", got, len([]rune(got)))
	}
	list, err := h.client.ListContacts(h.ctx(t), &kagamiv1.ListContactsRequest{})
	if err != nil || len(list.GetContacts()) != 1 || list.GetContacts()[0].GetFullName() != strings.TrimSpace(longName) {
		t.Fatalf("stored name %q, err %v; want the whole name kept", list.GetContacts()[0].GetFullName(), err)
	}
}

func (h *harness) relationshipOf(t *testing.T, name string) string {
	t.Helper()
	list, err := h.client.ListContacts(h.ctx(t), &kagamiv1.ListContactsRequest{Query: name})
	if err != nil || len(list.GetContacts()) != 1 {
		t.Fatalf("find %q: %v, %v", name, list.GetContacts(), err)
	}
	return list.GetContacts()[0].GetRelationship()
}

func TestAnImportStoresTheTypeThePreviewGuessedForNewPeople(t *testing.T) {
	h := newHarness(t)
	csv := sheet(
		[6]string{"Sana Iyer", "Lumen", "Technical Recruiter", "sana@lumen.example", "", ""},
		[6]string{"Tom Becker", "Corvid", "Engineering Manager", "tom@corvid.example", "", ""},
		[6]string{"Ana Ruiz", "Northwind", "Backend Engineer", "ana@northwind.example", "", ""},
		[6]string{"Lee Park", "Northwind", "Designer", "lee@northwind.example", "", ""},
		[6]string{"Kim Soo", "Northwind", "Technical Recruiter", "kim@northwind.example", "", "friend"},
		[6]string{"Pat Doe", "Northwind", "", "pat@northwind.example", "", ""},
	)
	preview := h.preview(t, csv)

	h.importCSV(t, csv, false)

	want := map[string]string{"Sana Iyer": "recruiter", "Tom Becker": "manager", "Ana Ruiz": "engineer", "Lee Park": "other", "Kim Soo": "friend", "Pat Doe": ""}
	for name, rel := range want {
		if got := h.relationshipOf(t, name); got != rel {
			t.Errorf("%s stored as %q, want %q", name, got, rel)
		}
	}
	// What the preview tagged is what was stored, apart from "no title, no guess":
	// the preview shows other for it and nothing is stored.
	for _, r := range preview {
		stored := h.relationshipOf(t, r.GetFullName())
		if r.GetRole() == "" {
			continue
		}
		if stored != r.GetRelationship() {
			t.Errorf("%s: the preview said %q and %q was stored", r.GetFullName(), r.GetRelationship(), stored)
		}
	}
}

func TestAnImportNeverChangesAnExistingPersonsTypeWithAGuess(t *testing.T) {
	h := newHarness(t)
	existing, err := h.client.AddContact(h.ctx(t), &kagamiv1.AddContactRequest{
		Contact: &kagamiv1.Contact{FullName: "Priya Raman", Email: "priya@lumen.example", Relationship: "friend", Role: "Friend"}, CompanyName: "Lumen",
	})
	if err != nil {
		t.Fatalf("add contact: %v", err)
	}
	noType := h.addContact(t, "Arjun Mehta", "arjun@corvid.example", "")

	h.importCSV(t, sheet(
		[6]string{"Priya Raman", "Lumen", "Technical Recruiter", "priya@lumen.example", "", ""},
		[6]string{"Arjun Mehta", "Corvid", "Engineering Manager", "arjun@corvid.example", "", ""},
	), false)

	if got := h.relationshipOf(t, "Priya Raman"); got != "friend" {
		t.Errorf("an existing person's type became %q, want it kept as friend (%s)", got, existing.GetContact().GetId())
	}
	if got := h.relationshipOf(t, "Arjun Mehta"); got != "" {
		t.Errorf("a person with no type was given %q by a guess on update (%s); only new people get one", got, noType.GetId())
	}
}
