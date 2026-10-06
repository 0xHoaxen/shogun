package grpc_test

import (
	"context"
	"testing"

	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
)

// payloadOf returns the payload of the only outbox event of a type.
func (h *harness) payloadOf(t *testing.T, eventType string, into proto.Message) {
	t.Helper()
	var raw []byte
	if err := h.pool.QueryRow(context.Background(), `SELECT payload FROM outbox WHERE type = $1`, eventType).Scan(&raw); err != nil {
		t.Fatalf("read %s: %v", eventType, err)
	}
	var env eventsv1.Envelope
	if err := proto.Unmarshal(raw, &env); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}
	if err := env.GetPayload().UnmarshalTo(into); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
}

func TestJobAddedEventNamesTheOwner(t *testing.T) {
	h := newHarness(t)
	res := h.addJob(t, "Backend", "")

	var got kagamiv1.JobAdded
	h.payloadOf(t, "job.added", &got)

	if got.GetOwnerId() != h.owner || got.GetJobId() != res.GetJob().GetId() {
		t.Fatalf("got %+v, want owner %s and job %s", &got, h.owner, res.GetJob().GetId())
	}
}

func TestContactStatusChangedEventNamesTheOwnerAndPreferredChannel(t *testing.T) {
	h := newHarness(t)
	added, err := h.client.AddContact(h.ctx(t), &kagamiv1.AddContactRequest{
		Contact:     &kagamiv1.Contact{FullName: "Mira Okafor", Email: "mira@corvid.example", PreferredChannel: "linkedin"},
		CompanyName: "Corvid",
	})
	if err != nil {
		t.Fatalf("add contact: %v", err)
	}
	c := added.GetContact()

	if _, err := h.client.ChangeContactStatus(h.ctx(t), &kagamiv1.ChangeContactStatusRequest{
		Id: c.GetId(), ToStatus: kagamiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT, Version: c.GetVersion(),
	}); err != nil {
		t.Fatalf("change status: %v", err)
	}
	var got kagamiv1.ContactStatusChanged
	h.payloadOf(t, "contact.status_changed", &got)

	if got.GetOwnerId() != h.owner || got.GetChannel() != "linkedin" || got.GetContactId() != c.GetId() ||
		got.GetTo() != kagamiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT {
		t.Fatalf("got %+v", &got)
	}
}
