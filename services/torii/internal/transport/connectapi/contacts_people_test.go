package connectapi_test

import (
	"context"
	"testing"

	"connectrpc.com/connect"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
)

func TestGetContactReturnsTheContactItsHistoryAndTheJobsAtItsCompany(t *testing.T) {
	// Arrange
	h := newContactsHarness(t)
	h.kagami.get = func(*kagamiv1.GetContactRequest) (*kagamiv1.GetContactResponse, error) {
		c := kagamiContact("c1", "Priya Raman", "Lumen", kagamiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT)
		c.Relationship, c.HowWeMet = "recruiter", "GopherCon"
		return &kagamiv1.GetContactResponse{
			Contact: c,
			Timeline: []*kagamiv1.ContactEvent{
				{Id: "e2", Kind: "note", Payload: `{"text": "Wants a referral note"}`, OccurredAt: timestamppb.Now()},
				{Id: "e1", Kind: "status_changed", Payload: `{"manual": true}`, OccurredAt: timestamppb.Now()},
				{Id: "e0", Kind: "note", Payload: `not json`, OccurredAt: timestamppb.Now()},
			},
			LinkedJobs: []*kagamiv1.Job{{Id: "j1", Title: "Backend Engineer", CompanyName: "Lumen", Status: kagamiv1.JobStatus_JOB_STATUS_APPLIED, Version: 3}},
		}, nil
	}

	// Act
	resp, err := h.client.GetContact(context.Background(), withCookie(connect.NewRequest(&apiv1.GetContactRequest{Id: "c1"}), h.token))
	// Assert
	if err != nil {
		t.Fatalf("GetContact: %v", err)
	}
	if c := resp.Msg.GetContact(); c.GetRelationship() != "recruiter" || c.GetHowWeMet() != "GopherCon" {
		t.Fatalf("contact = %v, want the new fields", c)
	}
	h2 := resp.Msg.GetHistory()
	if len(h2) != 3 || h2[0].GetText() != "Wants a referral note" || h2[1].GetText() != "" || h2[2].GetText() != "" {
		t.Fatalf("history = %v, want the note's text, and none for other kinds or a payload that is not a note's", h2)
	}
	jobs := resp.Msg.GetLinkedJobs()
	if len(jobs) != 1 || jobs[0].GetTitle() != "Backend Engineer" || jobs[0].GetStatus() != apiv1.JobStatus_JOB_STATUS_APPLIED {
		t.Fatalf("linked jobs = %v", jobs)
	}
	if h.kagami.requestsSeen()[0].(*kagamiv1.GetContactRequest).GetId() != "c1" || h.kagami.ownersSeen()[0] != h.owner {
		t.Fatalf("kagami saw %v for %v", h.kagami.requestsSeen(), h.kagami.ownersSeen())
	}
}

func TestAddContactNoteAndMarkContactedForwardAndKeepReasons(t *testing.T) {
	h := newContactsHarness(t)
	h.kagami.note = func(in *kagamiv1.AddContactNoteRequest) (*kagamiv1.AddContactNoteResponse, error) {
		if in.GetText() == "" {
			return nil, grpcError(codes.InvalidArgument, "INVALID_NOTE", "write something to add a note")
		}
		return &kagamiv1.AddContactNoteResponse{Note: &kagamiv1.ContactEvent{Id: "n1", Kind: "note", Payload: `{"text":"hi"}`}}, nil
	}
	h.kagami.mark = func(in *kagamiv1.MarkContactedRequest) (*kagamiv1.MarkContactedResponse, error) {
		switch in.GetOn() {
		case "2999-01-01":
			return nil, grpcError(codes.InvalidArgument, "INVALID_DATE", "the date cannot be in the future")
		case "stale":
			return nil, grpcError(codes.Aborted, "VERSION_CONFLICT", "version is stale")
		}
		c := kagamiContact(in.GetId(), "Priya Raman", "Lumen", kagamiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT)
		c.LastContacted = in.GetOn()
		return &kagamiv1.MarkContactedResponse{Contact: c}, nil
	}

	note, err := h.client.AddContactNote(context.Background(), withCookie(connect.NewRequest(&apiv1.AddContactNoteRequest{Id: "c1", Text: "hi"}), h.token))
	_, empty := h.client.AddContactNote(context.Background(), withCookie(connect.NewRequest(&apiv1.AddContactNoteRequest{Id: "c1"}), h.token))
	marked, markErr := h.client.MarkContacted(context.Background(), withCookie(connect.NewRequest(&apiv1.MarkContactedRequest{Id: "c1", Version: 4, On: "2026-10-20"}), h.token))
	_, future := h.client.MarkContacted(context.Background(), withCookie(connect.NewRequest(&apiv1.MarkContactedRequest{Id: "c1", Version: 4, On: "2999-01-01"}), h.token))
	_, stale := h.client.MarkContacted(context.Background(), withCookie(connect.NewRequest(&apiv1.MarkContactedRequest{Id: "c1", Version: 1, On: "stale"}), h.token))

	if err != nil || note.Msg.GetNote().GetText() != "hi" {
		t.Fatalf("note = %v, err %v", note, err)
	}
	if markErr != nil || marked.Msg.GetContact().GetLastContacted() != "2026-10-20" ||
		marked.Msg.GetContact().GetStatus() != apiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT {
		t.Fatalf("marked = %v, err %v", marked, markErr)
	}
	for name, c := range map[string]struct {
		err    error
		code   connect.Code
		reason string
	}{
		"empty note":    {empty, connect.CodeInvalidArgument, "INVALID_NOTE"},
		"future date":   {future, connect.CodeInvalidArgument, "INVALID_DATE"},
		"stale version": {stale, connect.CodeAborted, "VERSION_CONFLICT"},
	} {
		if connect.CodeOf(c.err) != c.code || reasonOf(c.err) != c.reason {
			t.Errorf("%s: code %v reason %q, want %v %s", name, connect.CodeOf(c.err), reasonOf(c.err), c.code, c.reason)
		}
	}
}

func TestAddContactForwardsRelationshipAndHowWeMetAndKeepsItsReason(t *testing.T) {
	h := newContactsHarness(t)
	h.kagami.add = func(in *kagamiv1.AddContactRequest) (*kagamiv1.AddContactResponse, error) {
		if in.GetContact().GetRelationship() == "colleague" {
			return nil, grpcError(codes.InvalidArgument, "INVALID_RELATIONSHIP", "relationship must be one of the six")
		}
		return &kagamiv1.AddContactResponse{Contact: kagamiContact("c9", in.GetContact().GetFullName(), "", kagamiv1.ContactStatus_CONTACT_STATUS_NOT_REACHED)}, nil
	}

	_, ok := h.client.AddContact(context.Background(), withCookie(connect.NewRequest(&apiv1.AddContactRequest{FullName: "Asha", Relationship: "friend", HowWeMet: "college"}), h.token))
	_, bad := h.client.AddContact(context.Background(), withCookie(connect.NewRequest(&apiv1.AddContactRequest{FullName: "Asha", Relationship: "colleague"}), h.token))

	sent := h.kagami.requestsSeen()[0].(*kagamiv1.AddContactRequest).GetContact()
	if ok != nil || sent.GetRelationship() != "friend" || sent.GetHowWeMet() != "college" {
		t.Fatalf("err %v, sent %v", ok, sent)
	}
	if connect.CodeOf(bad) != connect.CodeInvalidArgument || reasonOf(bad) != "INVALID_RELATIONSHIP" {
		t.Fatalf("code %v reason %q", connect.CodeOf(bad), reasonOf(bad))
	}
}

func TestImportContactsPassesTheUsersMappingAndSelectionAndReturnsThePreview(t *testing.T) {
	h := newContactsHarness(t)
	h.kagami.imp = func(_ *kagamiv1.ImportContactsRequest) (*kagamiv1.ImportContactsResponse, error) {
		return &kagamiv1.ImportContactsResponse{
			Report:    &kagamiv1.ImportReport{RowsTotal: 1, RowsCreated: 1, RowsSkipped: 2},
			DateOrder: "dmy",
			Rows: []*kagamiv1.ImportPreviewRow{{
				Row: 2, FullName: "Priya Raman", Role: "Recruiter", Company: "Lumen", Email: "p@lumen.example", Relationship: "recruiter",
				RelationshipGuessed: true, Duplicate: true, DuplicateContactId: "c1", DuplicateReason: "email", RepeatsRow: 0, Problem: "",
			}},
			RowsTruncated: true,
		}, nil
	}

	resp, err := h.client.ImportContacts(context.Background(), withCookie(connect.NewRequest(&apiv1.ImportContactsRequest{
		Csv: []byte("Name\nPriya\n"), DryRun: true, DateOrder: "dmy",
		Mappings:  []*apiv1.ColumnMapping{{Target: "full_name", Sources: []string{"Name"}}},
		Selection: &apiv1.RowSelection{Rows: []int32{2, 4}},
	}), h.token))
	if err != nil {
		t.Fatalf("ImportContacts: %v", err)
	}
	sent := h.kagami.requestsSeen()[0].(*kagamiv1.ImportContactsRequest)
	if len(sent.GetMappings()) != 1 || sent.GetMappings()[0].GetTarget() != "full_name" || sent.GetDateOrder() != "dmy" ||
		sent.GetSelection() == nil || len(sent.GetSelection().GetRows()) != 2 {
		t.Fatalf("kagami request = %v", sent)
	}
	r := resp.Msg.GetRows()
	if len(r) != 1 || !r[0].GetDuplicate() || r[0].GetDuplicateContactId() != "c1" || r[0].GetDuplicateReason() != "email" ||
		!r[0].GetRelationshipGuessed() || r[0].GetRelationship() != "recruiter" || r[0].GetCompany() != "Lumen" {
		t.Fatalf("rows = %v", r)
	}
	if !resp.Msg.GetRowsTruncated() || resp.Msg.GetDateOrder() != "dmy" || resp.Msg.GetReport().GetRowsSkipped() != 2 {
		t.Fatalf("response = %v", resp.Msg)
	}
}

func TestNoSelectionStaysNoSelectionAndAnEmptyOneStaysEmpty(t *testing.T) {
	h := newContactsHarness(t)
	h.kagami.imp = func(*kagamiv1.ImportContactsRequest) (*kagamiv1.ImportContactsResponse, error) {
		return &kagamiv1.ImportContactsResponse{Report: &kagamiv1.ImportReport{}}, nil
	}
	call := func(sel *apiv1.RowSelection) *kagamiv1.ImportContactsRequest {
		if _, err := h.client.ImportContacts(context.Background(), withCookie(connect.NewRequest(&apiv1.ImportContactsRequest{Csv: []byte("x"), Selection: sel}), h.token)); err != nil {
			t.Fatalf("ImportContacts: %v", err)
		}
		seen := h.kagami.requestsSeen()
		return seen[len(seen)-1].(*kagamiv1.ImportContactsRequest)
	}

	none := call(nil)
	empty := call(&apiv1.RowSelection{})

	if none.GetSelection() != nil {
		t.Errorf("no selection became %v: it would import nothing instead of everything", none.GetSelection())
	}
	if empty.GetSelection() == nil || len(empty.GetSelection().GetRows()) != 0 {
		t.Errorf("an empty selection became %v: it would import everything instead of nothing", empty.GetSelection())
	}
}

func TestImportContactsKeepsTheNewRefusalReasons(t *testing.T) {
	for _, reason := range []string{"INVALID_MAPPING", "INVALID_ROW_SELECTION", "IMPORT_NO_NAME_COLUMN"} {
		h := newContactsHarness(t)
		h.kagami.imp = func(*kagamiv1.ImportContactsRequest) (*kagamiv1.ImportContactsResponse, error) {
			return nil, grpcError(codes.InvalidArgument, reason, "refused")
		}

		_, err := h.client.ImportContacts(context.Background(), withCookie(connect.NewRequest(&apiv1.ImportContactsRequest{Csv: []byte("x")}), h.token))

		if reasonOf(err) != reason {
			t.Errorf("reason %q, want %q", reasonOf(err), reason)
		}
	}
}

func TestPeopleCallsNeedASession(t *testing.T) {
	h := newContactsHarness(t)

	_, get := h.client.GetContact(context.Background(), connect.NewRequest(&apiv1.GetContactRequest{Id: "c1"}))
	_, note := h.client.AddContactNote(context.Background(), connect.NewRequest(&apiv1.AddContactNoteRequest{Id: "c1", Text: "x"}))
	_, mark := h.client.MarkContacted(context.Background(), connect.NewRequest(&apiv1.MarkContactedRequest{Id: "c1", Version: 1}))

	for name, err := range map[string]error{"get": get, "note": note, "mark": mark} {
		if connect.CodeOf(err) != connect.CodeUnauthenticated {
			t.Errorf("%s: code %v, want Unauthenticated", name, connect.CodeOf(err))
		}
	}
	if len(h.kagami.requestsSeen()) != 0 {
		t.Fatal("kagami was called without a session")
	}
}
