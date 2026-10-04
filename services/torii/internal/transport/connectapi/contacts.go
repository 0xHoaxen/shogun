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
		Filename: in.GetFilename(),
		Csv:      in.GetCsv(),
		DryRun:   in.GetDryRun(),
	})
	if err != nil {
		return nil, fromGRPC(ctx, s.log, err)
	}
	return connect.NewResponse(&apiv1.ImportContactsResponse{
		Report:   importReportToAPI(resp.GetReport()),
		ImportId: resp.GetImportId(),
	}), nil
}
