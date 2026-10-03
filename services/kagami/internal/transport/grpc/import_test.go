package grpc_test

import (
	"context"
	"testing"

	"google.golang.org/grpc/codes"

	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
)

const csvHeader = "full_name,company,role,email,linkedin_url,x_handle,phone,relationship,how_we_met," +
	"status,preferred_channel,last_contacted,next_follow_up,target_role,job_link,tags,notes\n"

// goodAndBadRows has two good rows, one with no name and one with a bad status.
const goodAndBadRows = csvHeader +
	"Priya Raman,Lumen,Staff Engineer,priya@lumen.example,,,,recruiter,,,email,,,,,go;referral,\n" +
	"Arjun Mehta,Corvid,Engineering Manager,arjun@corvid.example,,,,manager,,reached_out,,,,,,,\n" +
	",Tessellate,Recruiter,nobody@tessellate.example,,,,,,,,,,,,,\n" +
	"Kenji Watanabe,Northwind,,kenji@northwind.example,,,,,,ghosted,,,,,,,\n"

// rowCount counts the rows of one of the kagami tables.
func (h *harness) rowCount(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := h.pool.QueryRow(context.Background(), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func (h *harness) importCSV(t *testing.T, csv string, dryRun bool) *kagamiv1.ImportContactsResponse {
	t.Helper()
	res, err := h.client.ImportContacts(h.ctx(t), &kagamiv1.ImportContactsRequest{
		Filename: "contacts-sept.csv", Csv: []byte(csv), DryRun: dryRun,
	})
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	return res
}

func TestImportDryRunReportsAndWritesNothing(t *testing.T) {
	h := newHarness(t)

	res := h.importCSV(t, goodAndBadRows, true)

	report := res.GetReport()
	if report.GetRowsTotal() != 4 || report.GetRowsCreated() != 2 || report.GetRowsUpdated() != 0 || report.GetRowsFailed() != 2 {
		t.Fatalf("got %+v", report)
	}
	if res.GetImportId() != "" {
		t.Fatalf("dry run returned import id %q", res.GetImportId())
	}
	if len(report.GetErrors()) != 2 {
		t.Fatalf("got errors %+v, want two", report.GetErrors())
	}
	first, second := report.GetErrors()[0], report.GetErrors()[1]
	if first.GetRow() != 4 || first.GetColumn() != "full_name" || second.GetRow() != 5 || second.GetColumn() != "status" {
		t.Fatalf("got %+v and %+v, want full_name on row 4 and status on row 5", first, second)
	}
	for _, table := range []string{"contacts", "companies", "contact_events", "imports", "outbox"} {
		if n := h.rowCount(t, table); n != 0 {
			t.Fatalf("dry run left %d rows in %s", n, table)
		}
	}
}

func TestImportCommitCreatesContactsAndOneEventEach(t *testing.T) {
	h := newHarness(t)

	res := h.importCSV(t, goodAndBadRows, false)

	if res.GetImportId() == "" || res.GetReport().GetRowsCreated() != 2 || res.GetReport().GetRowsFailed() != 2 {
		t.Fatalf("got %+v", res)
	}
	if n := h.rowCount(t, "contacts"); n != 2 {
		t.Fatalf("got %d contacts, want 2", n)
	}
	if n := h.outboxCount(t, "contact.added"); n != 2 {
		t.Fatalf("got %d contact.added rows, want 2", n)
	}
	var total, created, failed int
	err := h.pool.QueryRow(context.Background(),
		`SELECT rows_total, rows_created, rows_failed FROM imports WHERE id = $1`, res.GetImportId()).Scan(&total, &created, &failed)
	if err != nil || total != 4 || created != 2 || failed != 2 {
		t.Fatalf("imports row: %d/%d/%d, %v", total, created, failed, err)
	}

	list, err := h.client.ListContacts(h.ctx(t), &kagamiv1.ListContactsRequest{
		Status: kagamiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT,
	})
	if err != nil || len(list.GetContacts()) != 1 || list.GetContacts()[0].GetFullName() != "Arjun Mehta" {
		t.Fatalf("status from the CSV was not kept: %+v, %v", list.GetContacts(), err)
	}
}

func TestReimportUpdatesInsteadOfDuplicating(t *testing.T) {
	h := newHarness(t)
	h.importCSV(t, goodAndBadRows, false)
	before := h.outboxCount(t, "contact.added")

	again := csvHeader +
		"Priya R.,Lumen,Principal Engineer,PRIYA@lumen.example,,,,,,,,,,,,,\n" +
		"Arjun Mehta,,,arjun@corvid.example,,,,,,replied,,,,,,,\n"
	res := h.importCSV(t, again, false)

	if res.GetReport().GetRowsCreated() != 0 || res.GetReport().GetRowsUpdated() != 2 || res.GetReport().GetRowsFailed() != 0 {
		t.Fatalf("got %+v", res.GetReport())
	}
	if n := h.rowCount(t, "contacts"); n != 2 {
		t.Fatalf("got %d contacts after a re-import, want 2", n)
	}
	if n := h.outboxCount(t, "contact.added"); n != before {
		t.Fatalf("re-import wrote %d new contact.added rows, want none", n-before)
	}

	list, err := h.client.ListContacts(h.ctx(t), &kagamiv1.ListContactsRequest{Query: "priya"})
	if err != nil || len(list.GetContacts()) != 1 {
		t.Fatalf("find priya: %+v, %v", list.GetContacts(), err)
	}
	priya := list.GetContacts()[0]
	if priya.GetFullName() != "Priya R." || priya.GetRole() != "Principal Engineer" ||
		priya.GetRelationship() != "recruiter" || priya.GetPreferredChannel() != "email" {
		t.Fatalf("empty cells must keep stored values, got %+v", priya)
	}
	arjuns, err := h.client.ListContacts(h.ctx(t), &kagamiv1.ListContactsRequest{Query: "arjun"})
	if err != nil || arjuns.GetContacts()[0].GetStatus() != kagamiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT {
		t.Fatalf("re-import must not change the status: %+v, %v", arjuns.GetContacts(), err)
	}
}

func TestImportLinksContactsToJobsByURL(t *testing.T) {
	h := newHarness(t)
	job := h.addJob(t, "Backend Engineer", "").GetJob()

	data := csvHeader +
		"Daniel Sousa,Northwind,Senior SWE,daniel@northwind.example,,,,,,,,,,,https://northwind.example/jobs/Backend Engineer,,\n" +
		"Asha Nair,Northwind,,asha@northwind.example,,,,,,,,,,,https://northwind.example/jobs/missing,,\n"
	res := h.importCSV(t, data, false)

	report := res.GetReport()
	if report.GetRowsCreated() != 1 || report.GetRowsFailed() != 1 ||
		report.GetErrors()[0].GetColumn() != "job_link" || report.GetErrors()[0].GetRow() != 3 {
		t.Fatalf("got %+v", report)
	}
	list, err := h.client.ListContacts(h.ctx(t), &kagamiv1.ListContactsRequest{Query: "daniel"})
	if err != nil || len(list.GetContacts()) != 1 || list.GetContacts()[0].GetJobId() != job.GetId() {
		t.Fatalf("contact not linked to the job: %+v, %v", list.GetContacts(), err)
	}
}

func TestImportRefusesAFileThatIsNotAContactsCSV(t *testing.T) {
	h := newHarness(t)
	tests := []struct {
		name   string
		csv    string
		reason string
	}{
		{"empty", "", "IMPORT_EMPTY"},
		{"wrong header", "name,email\nA,a@x.example\n", "INVALID_HEADER"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.client.ImportContacts(h.ctx(t), &kagamiv1.ImportContactsRequest{Csv: []byte(tt.csv)})
			requireStatus(t, err, codes.InvalidArgument, tt.reason)
		})
	}
	if n := h.rowCount(t, "imports"); n != 0 {
		t.Fatalf("refused files left %d imports rows", n)
	}
}
