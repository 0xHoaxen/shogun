package jobs

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
	"github.com/0xHoaxen/shogun/services/kagami/internal/app"
	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
)

// payloads returns the decoded payload of every outbox event of a type.
func (e *scanEnv) payloads(t *testing.T, eventType string, into func() proto.Message) []proto.Message {
	t.Helper()
	rows, err := e.pool.Query(context.Background(), `SELECT payload FROM outbox WHERE type = $1`, eventType)
	if err != nil {
		t.Fatalf("read %s: %v", eventType, err)
	}
	defer rows.Close()
	var out []proto.Message
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			t.Fatalf("scan payload: %v", err)
		}
		var env eventsv1.Envelope
		if err := proto.Unmarshal(raw, &env); err != nil {
			t.Fatalf("unmarshal envelope: %v", err)
		}
		msg := into()
		if err := env.GetPayload().UnmarshalTo(msg); err != nil {
			t.Fatalf("unmarshal payload: %v", err)
		}
		out = append(out, msg)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows: %v", err)
	}
	return out
}

func TestFollowUpDueEventsNameTheirOwners(t *testing.T) {
	env := newScanEnv(t)
	jobOwner, contactOwner := uuid.New(), uuid.New()
	jobID := env.addJob(t, jobOwner, "Due today", domain.JobSaved)
	env.setJobFollowUp(t, jobOwner, jobID, "2026-10-03")
	if _, err := env.svc.AddContact(env.as(contactOwner), app.AddContactInput{
		ContactFields: app.ContactFields{FullName: "Arjun Mehta", NextFollowUp: "2026-10-03"},
	}); err != nil {
		t.Fatalf("add contact: %v", err)
	}

	if _, err := env.svc.ScanDueFollowUps(context.Background(), time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("scan: %v", err)
	}

	jobEvents := env.payloads(t, "job.follow_up_due", func() proto.Message { return &kagamiv1.JobFollowUpDue{} })
	contactEvents := env.payloads(t, "contact.follow_up_due", func() proto.Message { return &kagamiv1.ContactFollowUpDue{} })
	var jobOwners, contactOwners []string
	for _, m := range jobEvents {
		jobOwners = append(jobOwners, m.(*kagamiv1.JobFollowUpDue).GetOwnerId())
	}
	for _, m := range contactEvents {
		contactOwners = append(contactOwners, m.(*kagamiv1.ContactFollowUpDue).GetOwnerId())
	}
	if !slices.Equal(jobOwners, []string{jobOwner.String()}) {
		t.Fatalf("job.follow_up_due owners %v, want [%s]", jobOwners, jobOwner)
	}
	if !slices.Equal(contactOwners, []string{contactOwner.String()}) {
		t.Fatalf("contact.follow_up_due owners %v, want [%s]", contactOwners, contactOwner)
	}
}
