package connectapi

import (
	"context"
	"log/slog"

	"connectrpc.com/connect"
	"google.golang.org/grpc"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	"github.com/0xHoaxen/shogun/gen/go/shogun/api/v1/apiv1connect"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
)

const (
	// maxImportBytes matches kagami's import cap, which stays under gRPC's 4 MiB
	// message limit. Checking here turns an oversize file into a clear error
	// instead of a ResourceExhausted from the transport.
	maxImportBytes = 3 << 20

	reasonImportTooLarge = "IMPORT_TOO_LARGE"
)

// ContactsBackend is the part of kagami's client ContactsServer uses.
type ContactsBackend interface {
	ListContacts(ctx context.Context, in *kagamiv1.ListContactsRequest, opts ...grpc.CallOption) (*kagamiv1.ListContactsResponse, error)
	AddContact(ctx context.Context, in *kagamiv1.AddContactRequest, opts ...grpc.CallOption) (*kagamiv1.AddContactResponse, error)
	ChangeContactStatus(ctx context.Context, in *kagamiv1.ChangeContactStatusRequest, opts ...grpc.CallOption) (*kagamiv1.ChangeContactStatusResponse, error)
	ImportContacts(ctx context.Context, in *kagamiv1.ImportContactsRequest, opts ...grpc.CallOption) (*kagamiv1.ImportContactsResponse, error)
	GetContact(ctx context.Context, in *kagamiv1.GetContactRequest, opts ...grpc.CallOption) (*kagamiv1.GetContactResponse, error)
	AddContactNote(ctx context.Context, in *kagamiv1.AddContactNoteRequest, opts ...grpc.CallOption) (*kagamiv1.AddContactNoteResponse, error)
	MarkContacted(ctx context.Context, in *kagamiv1.MarkContactedRequest, opts ...grpc.CallOption) (*kagamiv1.MarkContactedResponse, error)
}

// ContactsServer implements shogun.api.v1.ContactsService on top of kagami.
type ContactsServer struct {
	kagami ContactsBackend
	log    *slog.Logger
}

var _ apiv1connect.ContactsServiceHandler = (*ContactsServer)(nil)

// NewContactsServer returns a ContactsServer that calls kagami through backend.
func NewContactsServer(backend ContactsBackend, log *slog.Logger) *ContactsServer {
	return &ContactsServer{kagami: backend, log: log}
}

// ListContacts returns one page of contacts, optionally filtered.
func (s *ContactsServer) ListContacts(
	ctx context.Context, req *connect.Request[apiv1.ListContactsRequest],
) (*connect.Response[apiv1.ListContactsResponse], error) {
	in := req.Msg
	resp, err := s.kagami.ListContacts(ctx, &kagamiv1.ListContactsRequest{
		Status:    contactStatusToKagami(in.GetStatus()),
		Query:     in.GetQuery(),
		PageSize:  in.GetPageSize(),
		PageToken: in.GetPageToken(),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.ListContactsResponse{
		Contacts:      contactsToAPI(resp.GetContacts()),
		NextPageToken: resp.GetNextPageToken(),
	}), nil
}

// AddContact creates a contact. A repeated Idempotency-Key returns the original.
func (s *ContactsServer) AddContact(
	ctx context.Context, req *connect.Request[apiv1.AddContactRequest],
) (*connect.Response[apiv1.AddContactResponse], error) {
	in := req.Msg
	resp, err := s.kagami.AddContact(ctx, &kagamiv1.AddContactRequest{
		Contact: &kagamiv1.Contact{
			FullName:         in.GetFullName(),
			Role:             in.GetRole(),
			Email:            in.GetEmail(),
			LinkedinUrl:      in.GetLinkedinUrl(),
			PreferredChannel: in.GetPreferredChannel(),
			Notes:            in.GetNotes(),
			Relationship:     in.GetRelationship(),
			HowWeMet:         in.GetHowWeMet(),
		},
		CompanyName:    in.GetCompanyName(),
		IdempotencyKey: req.Header().Get(idempotencyKeyHeader),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.AddContactResponse{Contact: contactToAPI(resp.GetContact())}), nil
}

// ChangeContactStatus moves a contact along the state machine. An invalid move
// comes back as FailedPrecondition with reason CONTACT_STATUS_INVALID_TRANSITION.
func (s *ContactsServer) ChangeContactStatus(
	ctx context.Context, req *connect.Request[apiv1.ChangeContactStatusRequest],
) (*connect.Response[apiv1.ChangeContactStatusResponse], error) {
	in := req.Msg
	resp, err := s.kagami.ChangeContactStatus(ctx, &kagamiv1.ChangeContactStatusRequest{
		Id:       in.GetId(),
		ToStatus: contactStatusToKagami(in.GetToStatus()),
		Version:  in.GetVersion(),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.ChangeContactStatusResponse{Contact: contactToAPI(resp.GetContact())}), nil
}

// ImportContacts previews (dry run) or applies a contacts CSV.
func (s *ContactsServer) ImportContacts(
	ctx context.Context, req *connect.Request[apiv1.ImportContactsRequest],
) (*connect.Response[apiv1.ImportContactsResponse], error) {
	in := req.Msg
	if len(in.GetCsv()) > maxImportBytes {
		return nil, newError(connect.CodeInvalidArgument, reasonImportTooLarge, "the file is too large to import")
	}
	resp, err := s.kagami.ImportContacts(ctx, &kagamiv1.ImportContactsRequest{
		Filename:  in.GetFilename(),
		Csv:       in.GetCsv(),
		DryRun:    in.GetDryRun(),
		Reshape:   in.GetReshape(),
		Mappings:  columnMappingsToKagami(in.GetMappings()),
		DateOrder: in.GetDateOrder(),
		Selection: rowSelectionToKagami(in.GetSelection()),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.ImportContactsResponse{
		Report:        importReportToAPI(resp.GetReport()),
		ImportId:      resp.GetImportId(),
		ShapedCsv:     resp.GetShapedCsv(),
		Mappings:      columnMappingsToAPI(resp.GetMappings()),
		DateOrder:     resp.GetDateOrder(),
		Rows:          importPreviewToAPI(resp.GetRows()),
		RowsTruncated: resp.GetRowsTruncated(),
	}), nil
}

// GetContact returns one contact with its history and the jobs at its company.
func (s *ContactsServer) GetContact(
	ctx context.Context, req *connect.Request[apiv1.GetContactRequest],
) (*connect.Response[apiv1.GetContactResponse], error) {
	resp, err := s.kagami.GetContact(ctx, &kagamiv1.GetContactRequest{Id: req.Msg.GetId()})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	jobs := make([]*apiv1.JobCard, 0, len(resp.GetLinkedJobs()))
	for _, j := range resp.GetLinkedJobs() {
		jobs = append(jobs, jobCardToAPI(j))
	}
	return connect.NewResponse(&apiv1.GetContactResponse{
		Contact: contactToAPI(resp.GetContact()), History: contactEventsToAPI(resp.GetTimeline()), LinkedJobs: jobs,
	}), nil
}

// AddContactNote adds a note to a contact's history.
func (s *ContactsServer) AddContactNote(
	ctx context.Context, req *connect.Request[apiv1.AddContactNoteRequest],
) (*connect.Response[apiv1.AddContactNoteResponse], error) {
	resp, err := s.kagami.AddContactNote(ctx, &kagamiv1.AddContactNoteRequest{Id: req.Msg.GetId(), Text: req.Msg.GetText()})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.AddContactNoteResponse{Note: contactEventToAPI(resp.GetNote())}), nil
}

// MarkContacted records that the owner reached the contact themselves.
func (s *ContactsServer) MarkContacted(
	ctx context.Context, req *connect.Request[apiv1.MarkContactedRequest],
) (*connect.Response[apiv1.MarkContactedResponse], error) {
	in := req.Msg
	resp, err := s.kagami.MarkContacted(ctx, &kagamiv1.MarkContactedRequest{Id: in.GetId(), Version: in.GetVersion(), On: in.GetOn()})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.MarkContactedResponse{Contact: contactToAPI(resp.GetContact())}), nil
}

func columnMappingsToKagami(in []*apiv1.ColumnMapping) []*kagamiv1.ColumnMapping {
	out := make([]*kagamiv1.ColumnMapping, 0, len(in))
	for _, m := range in {
		out = append(out, &kagamiv1.ColumnMapping{Target: m.GetTarget(), Sources: m.GetSources()})
	}
	return out
}

// rowSelectionToKagami keeps "no selection" distinct from "an empty one".
func rowSelectionToKagami(in *apiv1.RowSelection) *kagamiv1.RowSelection {
	if in == nil {
		return nil
	}
	return &kagamiv1.RowSelection{Rows: in.GetRows()}
}

func columnMappingsToAPI(in []*kagamiv1.ColumnMapping) []*apiv1.ColumnMapping {
	out := make([]*apiv1.ColumnMapping, 0, len(in))
	for _, m := range in {
		out = append(out, &apiv1.ColumnMapping{Target: m.GetTarget(), Sources: m.GetSources()})
	}
	return out
}
