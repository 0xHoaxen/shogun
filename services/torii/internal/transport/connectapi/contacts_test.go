package connectapi_test

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"connectrpc.com/connect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/pkg/authz"
	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/app/apptest"
	"github.com/0xHoaxen/shogun/services/torii/internal/transport/connectapi"
)

// fakeContactsKagami answers the contact calls from the funcs a test sets, and
// records the owner each call acted for and the requests it received.
type fakeContactsKagami struct {
	list   func(*kagamiv1.ListContactsRequest) (*kagamiv1.ListContactsResponse, error)
	add    func(*kagamiv1.AddContactRequest) (*kagamiv1.AddContactResponse, error)
	change func(*kagamiv1.ChangeContactStatusRequest) (*kagamiv1.ChangeContactStatusResponse, error)
	imp    func(*kagamiv1.ImportContactsRequest) (*kagamiv1.ImportContactsResponse, error)

	mu       sync.Mutex
	owners   []string
	requests []any
}

func (f *fakeContactsKagami) record(ctx context.Context, in any) {
	id, _ := authz.FromContext(ctx)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.owners = append(f.owners, id.OwnerID)
	f.requests = append(f.requests, in)
}

func (f *fakeContactsKagami) requestsSeen() []any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]any(nil), f.requests...)
}

func (f *fakeContactsKagami) ownersSeen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.owners...)
}

func (f *fakeContactsKagami) ListContacts(ctx context.Context, in *kagamiv1.ListContactsRequest, _ ...grpc.CallOption) (*kagamiv1.ListContactsResponse, error) {
	f.record(ctx, in)
	return f.list(in)
}

func (f *fakeContactsKagami) AddContact(ctx context.Context, in *kagamiv1.AddContactRequest, _ ...grpc.CallOption) (*kagamiv1.AddContactResponse, error) {
	f.record(ctx, in)
	return f.add(in)
}

func (f *fakeContactsKagami) ChangeContactStatus(ctx context.Context, in *kagamiv1.ChangeContactStatusRequest, _ ...grpc.CallOption) (*kagamiv1.ChangeContactStatusResponse, error) {
	f.record(ctx, in)
	return f.change(in)
}

func (f *fakeContactsKagami) ImportContacts(ctx context.Context, in *kagamiv1.ImportContactsRequest, _ ...grpc.CallOption) (*kagamiv1.ImportContactsResponse, error) {
	f.record(ctx, in)
	return f.imp(in)
}

type contactsHarness struct {
	kagami *fakeContactsKagami
	client apiv1connect.ContactsServiceClient
	token  string
	owner  string
}

func newContactsHarness(t *testing.T) *contactsHarness {
	t.Helper()
	now := time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	auth, err := app.NewAuth(apptest.NewMemSessions(),
		app.AuthConfig{AllowedEmails: []string{ownerEmail}, SessionTTL: sessionTTL},
		app.WithClock(func() time.Time { return now }),
	)
	if err != nil {
		t.Fatalf("NewAuth: %v", err)
	}
	token, session, err := auth.StartSession(context.Background(),
		app.Claims{Subject: "sub-1", Email: ownerEmail, EmailVerified: true})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	log := slog.New(slog.DiscardHandler)
	kagami := &fakeContactsKagami{}
	mux := http.NewServeMux()
	mux.Handle(apiv1connect.NewContactsServiceHandler(connectapi.NewContactsServer(kagami, log),
		connect.WithInterceptors(connectapi.NewSessionInterceptor(connectapi.InterceptorConfig{Auth: auth, Log: log}))))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &contactsHarness{
		kagami: kagami, token: token, owner: session.OwnerID.String(),
		client: apiv1connect.NewContactsServiceClient(srv.Client(), srv.URL),
	}
}

func kagamiContact(id, name, company string, s kagamiv1.ContactStatus) *kagamiv1.Contact {
	return &kagamiv1.Contact{
		Id: id, FullName: name, CompanyName: company, Status: s, Version: 2,
		Email: name + "@example.com", NextFollowUp: "2026-10-08",
	}
}

func TestListContactsForwardsTheFilterAndMapsRows(t *testing.T) {
	// Arrange
	h := newContactsHarness(t)
	h.kagami.list = func(*kagamiv1.ListContactsRequest) (*kagamiv1.ListContactsResponse, error) {
		return &kagamiv1.ListContactsResponse{
			Contacts: []*kagamiv1.Contact{
				kagamiContact("c1", "priya", "Lumen", kagamiv1.ContactStatus_CONTACT_STATUS_REPLIED),
			},
			NextPageToken: "next",
		}, nil
	}

	// Act
	resp, err := h.client.ListContacts(context.Background(), withCookie(connect.NewRequest(&apiv1.ListContactsRequest{
		Status: apiv1.ContactStatus_CONTACT_STATUS_REPLIED, Query: "pri", PageSize: 25, PageToken: "tok",
	}), h.token))
	// Assert
	if err != nil {
		t.Fatalf("ListContacts: %v", err)
	}
	got := resp.Msg.GetContacts()
	if len(got) != 1 || got[0].GetId() != "c1" || got[0].GetCompanyName() != "Lumen" ||
		got[0].GetStatus() != apiv1.ContactStatus_CONTACT_STATUS_REPLIED || got[0].GetVersion() != 2 ||
		resp.Msg.GetNextPageToken() != "next" {
		t.Fatalf("response = %v", resp.Msg)
	}
	sent, ok := h.kagami.requestsSeen()[0].(*kagamiv1.ListContactsRequest)
	if !ok || sent.GetStatus() != kagamiv1.ContactStatus_CONTACT_STATUS_REPLIED || sent.GetQuery() != "pri" ||
		sent.GetPageSize() != 25 || sent.GetPageToken() != "tok" {
		t.Fatalf("kagami request = %v", h.kagami.requestsSeen()[0])
	}
	if owners := h.kagami.ownersSeen(); !reflect.DeepEqual(owners, []string{h.owner}) {
		t.Fatalf("kagami call acted for %v, want the signed-in owner", owners)
	}
}

func TestAddContactForwardsTheFieldsAndTheIdempotencyKey(t *testing.T) {
	// Arrange
	h := newContactsHarness(t)
	h.kagami.add = func(in *kagamiv1.AddContactRequest) (*kagamiv1.AddContactResponse, error) {
		c := kagamiContact("c9", in.GetContact().GetFullName(), in.GetCompanyName(), kagamiv1.ContactStatus_CONTACT_STATUS_NOT_REACHED)
		return &kagamiv1.AddContactResponse{Contact: c}, nil
	}
	req := withCookie(connect.NewRequest(&apiv1.AddContactRequest{
		FullName: "Asha Nair", Role: "Hiring Manager", CompanyName: "Quayside", Email: "asha@quayside.example",
		LinkedinUrl: "https://linkedin.example/asha", PreferredChannel: "email", Notes: "met at meetup",
	}), h.token)
	req.Header().Set("Idempotency-Key", "key-1")

	// Act
	resp, err := h.client.AddContact(context.Background(), req)
	// Assert
	if err != nil {
		t.Fatalf("AddContact: %v", err)
	}
	if got := resp.Msg.GetContact(); got.GetId() != "c9" || got.GetCompanyName() != "Quayside" {
		t.Fatalf("contact = %v", got)
	}
	sent, ok := h.kagami.requestsSeen()[0].(*kagamiv1.AddContactRequest)
	c := sent.GetContact()
	if !ok || sent.GetIdempotencyKey() != "key-1" || sent.GetCompanyName() != "Quayside" ||
		c.GetFullName() != "Asha Nair" || c.GetRole() != "Hiring Manager" || c.GetEmail() != "asha@quayside.example" ||
		c.GetLinkedinUrl() != "https://linkedin.example/asha" || c.GetPreferredChannel() != "email" || c.GetNotes() != "met at meetup" {
		t.Fatalf("kagami request = %v", h.kagami.requestsSeen()[0])
	}
}

func TestChangeContactStatusKeepsDownstreamReasons(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantCode   connect.Code
		wantReason string
	}{
		{"invalid transition", grpcError(codes.FailedPrecondition, "CONTACT_STATUS_INVALID_TRANSITION", "cannot move"), connect.CodeFailedPrecondition, "CONTACT_STATUS_INVALID_TRANSITION"},
		{"stale version", grpcError(codes.Aborted, "VERSION_CONFLICT", "changed"), connect.CodeAborted, "VERSION_CONFLICT"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			h := newContactsHarness(t)
			h.kagami.change = func(*kagamiv1.ChangeContactStatusRequest) (*kagamiv1.ChangeContactStatusResponse, error) {
				return nil, tt.err
			}

			// Act
			_, err := h.client.ChangeContactStatus(context.Background(), withCookie(connect.NewRequest(&apiv1.ChangeContactStatusRequest{
				Id: "c1", ToStatus: apiv1.ContactStatus_CONTACT_STATUS_REFERRAL_ASKED, Version: 2,
			}), h.token))

			// Assert
			if connect.CodeOf(err) != tt.wantCode || reasonOf(err) != tt.wantReason {
				t.Fatalf("code %v reason %q, want %v %s", connect.CodeOf(err), reasonOf(err), tt.wantCode, tt.wantReason)
			}
		})
	}
}

func TestChangeContactStatusForwardsTheMove(t *testing.T) {
	// Arrange
	h := newContactsHarness(t)
	h.kagami.change = func(in *kagamiv1.ChangeContactStatusRequest) (*kagamiv1.ChangeContactStatusResponse, error) {
		c := kagamiContact(in.GetId(), "priya", "Lumen", in.GetToStatus())
		c.Version = in.GetVersion() + 1
		return &kagamiv1.ChangeContactStatusResponse{Contact: c}, nil
	}

	// Act
	resp, err := h.client.ChangeContactStatus(context.Background(), withCookie(connect.NewRequest(&apiv1.ChangeContactStatusRequest{
		Id: "c1", ToStatus: apiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT, Version: 2,
	}), h.token))
	// Assert
	if err != nil {
		t.Fatalf("ChangeContactStatus: %v", err)
	}
	if got := resp.Msg.GetContact(); got.GetStatus() != apiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT || got.GetVersion() != 3 {
		t.Fatalf("contact = %v", got)
	}
}

func TestImportContactsDryRunReturnsThePerRowReport(t *testing.T) {
	// Arrange
	h := newContactsHarness(t)
	h.kagami.imp = func(*kagamiv1.ImportContactsRequest) (*kagamiv1.ImportContactsResponse, error) {
		return &kagamiv1.ImportContactsResponse{Report: &kagamiv1.ImportReport{
			RowsTotal: 3, RowsCreated: 1, RowsUpdated: 1, RowsFailed: 1,
			Errors: []*kagamiv1.ImportRowError{{Row: 4, Column: "full_name", Message: "name is required"}},
		}}, nil
	}
	csv := []byte("full_name,email\nA,a@example.com\n")

	// Act
	resp, err := h.client.ImportContacts(context.Background(), withCookie(connect.NewRequest(&apiv1.ImportContactsRequest{
		Filename: "contacts.csv", Csv: csv, DryRun: true,
	}), h.token))
	// Assert
	if err != nil {
		t.Fatalf("ImportContacts: %v", err)
	}
	report := resp.Msg.GetReport()
	if report.GetRowsTotal() != 3 || report.GetRowsCreated() != 1 || report.GetRowsUpdated() != 1 || report.GetRowsFailed() != 1 ||
		len(report.GetErrors()) != 1 || report.GetErrors()[0].GetRow() != 4 ||
		report.GetErrors()[0].GetColumn() != "full_name" || report.GetErrors()[0].GetMessage() != "name is required" {
		t.Fatalf("report = %v", report)
	}
	if resp.Msg.GetImportId() != "" {
		t.Fatalf("import_id = %q, want empty on a dry run", resp.Msg.GetImportId())
	}
	sent, ok := h.kagami.requestsSeen()[0].(*kagamiv1.ImportContactsRequest)
	if !ok || !sent.GetDryRun() || sent.GetFilename() != "contacts.csv" || !bytes.Equal(sent.GetCsv(), csv) {
		t.Fatalf("kagami request = %v", h.kagami.requestsSeen()[0])
	}
}

func TestImportContactsConfirmReturnsTheImportID(t *testing.T) {
	// Arrange
	h := newContactsHarness(t)
	h.kagami.imp = func(*kagamiv1.ImportContactsRequest) (*kagamiv1.ImportContactsResponse, error) {
		return &kagamiv1.ImportContactsResponse{
			Report:   &kagamiv1.ImportReport{RowsTotal: 2, RowsCreated: 2},
			ImportId: "import-1",
		}, nil
	}

	// Act
	resp, err := h.client.ImportContacts(context.Background(), withCookie(connect.NewRequest(&apiv1.ImportContactsRequest{
		Filename: "contacts.csv", Csv: []byte("full_name\nA\nB\n"),
	}), h.token))
	// Assert
	if err != nil {
		t.Fatalf("ImportContacts: %v", err)
	}
	if resp.Msg.GetImportId() != "import-1" || resp.Msg.GetReport().GetRowsCreated() != 2 {
		t.Fatalf("response = %v", resp.Msg)
	}
	if sent := h.kagami.requestsSeen()[0].(*kagamiv1.ImportContactsRequest); sent.GetDryRun() {
		t.Fatal("confirm was sent as a dry run")
	}
}

func TestImportContactsRejectsAnOversizeFileWithoutCallingKagami(t *testing.T) {
	// Arrange
	h := newContactsHarness(t)
	csv := bytes.Repeat([]byte("x"), 3<<20+1)

	// Act
	_, err := h.client.ImportContacts(context.Background(), withCookie(connect.NewRequest(&apiv1.ImportContactsRequest{
		Filename: "big.csv", Csv: csv, DryRun: true,
	}), h.token))

	// Assert
	if connect.CodeOf(err) != connect.CodeInvalidArgument || reasonOf(err) != "IMPORT_TOO_LARGE" {
		t.Fatalf("code %v reason %q, want InvalidArgument IMPORT_TOO_LARGE", connect.CodeOf(err), reasonOf(err))
	}
	if len(h.kagami.requestsSeen()) != 0 {
		t.Fatal("kagami was called for an oversize file")
	}
}

func TestImportContactsKeepsKagamisRejectionReason(t *testing.T) {
	// Arrange
	h := newContactsHarness(t)
	h.kagami.imp = func(*kagamiv1.ImportContactsRequest) (*kagamiv1.ImportContactsResponse, error) {
		return nil, grpcError(codes.InvalidArgument, "IMPORT_MISSING_COLUMN", "the file has no full_name column")
	}

	// Act
	_, err := h.client.ImportContacts(context.Background(), withCookie(connect.NewRequest(&apiv1.ImportContactsRequest{
		Filename: "bad.csv", Csv: []byte("email\na@example.com\n"), DryRun: true,
	}), h.token))

	// Assert
	if connect.CodeOf(err) != connect.CodeInvalidArgument || reasonOf(err) != "IMPORT_MISSING_COLUMN" {
		t.Fatalf("code %v reason %q, want InvalidArgument IMPORT_MISSING_COLUMN", connect.CodeOf(err), reasonOf(err))
	}
}

func TestContactsRequireASession(t *testing.T) {
	// Arrange
	h := newContactsHarness(t)

	// Act
	_, err := h.client.ListContacts(context.Background(), connect.NewRequest(&apiv1.ListContactsRequest{}))

	// Assert
	if connect.CodeOf(err) != connect.CodeUnauthenticated {
		t.Fatalf("code = %v, want Unauthenticated", connect.CodeOf(err))
	}
	if len(h.kagami.requestsSeen()) != 0 {
		t.Fatal("kagami was called without a session")
	}
}
