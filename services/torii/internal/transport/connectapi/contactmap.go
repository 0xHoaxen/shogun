package connectapi

import (
	"encoding/json"

	apiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/api/v1"
	kagamiv1 "github.com/0xHoaxen/shogun/gen/go/shogun/kagami/v1"
)

// contactStatusPairs maps the browser API's contact statuses to kagami's.
var contactStatusPairs = []struct {
	api    apiv1.ContactStatus
	kagami kagamiv1.ContactStatus
}{
	{apiv1.ContactStatus_CONTACT_STATUS_NOT_REACHED, kagamiv1.ContactStatus_CONTACT_STATUS_NOT_REACHED},
	{apiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT, kagamiv1.ContactStatus_CONTACT_STATUS_REACHED_OUT},
	{apiv1.ContactStatus_CONTACT_STATUS_CONVERSATION_STARTED, kagamiv1.ContactStatus_CONTACT_STATUS_CONVERSATION_STARTED},
	{apiv1.ContactStatus_CONTACT_STATUS_REPLIED, kagamiv1.ContactStatus_CONTACT_STATUS_REPLIED},
	{apiv1.ContactStatus_CONTACT_STATUS_REFERRAL_ASKED, kagamiv1.ContactStatus_CONTACT_STATUS_REFERRAL_ASKED},
}

func contactStatusToAPI(s kagamiv1.ContactStatus) apiv1.ContactStatus {
	for _, pair := range contactStatusPairs {
		if pair.kagami == s {
			return pair.api
		}
	}
	return apiv1.ContactStatus_CONTACT_STATUS_UNSPECIFIED
}

func contactStatusToKagami(s apiv1.ContactStatus) kagamiv1.ContactStatus {
	for _, pair := range contactStatusPairs {
		if pair.api == s {
			return pair.kagami
		}
	}
	return kagamiv1.ContactStatus_CONTACT_STATUS_UNSPECIFIED
}

func contactToAPI(c *kagamiv1.Contact) *apiv1.Contact {
	return &apiv1.Contact{
		Id:               c.GetId(),
		FullName:         c.GetFullName(),
		Role:             c.GetRole(),
		CompanyName:      c.GetCompanyName(),
		Status:           contactStatusToAPI(c.GetStatus()),
		Email:            c.GetEmail(),
		LinkedinUrl:      c.GetLinkedinUrl(),
		PreferredChannel: c.GetPreferredChannel(),
		LastContacted:    c.GetLastContacted(),
		NextFollowUp:     c.GetNextFollowUp(),
		Notes:            c.GetNotes(),
		Version:          c.GetVersion(),
		Relationship:     c.GetRelationship(),
		HowWeMet:         c.GetHowWeMet(),
	}
}

// contactEventToAPI shows one timeline entry. kagami keeps a note's text in the
// entry's JSON payload; the web gets it as text and never sees the payload.
func contactEventToAPI(e *kagamiv1.ContactEvent) *apiv1.ContactEvent {
	var payload struct {
		Text string `json:"text"`
	}
	if e.GetKind() == "note" {
		// A payload that is not a note's JSON leaves the text empty.
		_ = json.Unmarshal([]byte(e.GetPayload()), &payload)
	}
	return &apiv1.ContactEvent{
		Id: e.GetId(), Kind: e.GetKind(), Channel: e.GetChannel(), Text: payload.Text, OccurredAt: e.GetOccurredAt(),
	}
}

func contactEventsToAPI(events []*kagamiv1.ContactEvent) []*apiv1.ContactEvent {
	out := make([]*apiv1.ContactEvent, 0, len(events))
	for _, e := range events {
		out = append(out, contactEventToAPI(e))
	}
	return out
}

func importPreviewToAPI(rows []*kagamiv1.ImportPreviewRow) []*apiv1.ImportPreviewRow {
	out := make([]*apiv1.ImportPreviewRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, &apiv1.ImportPreviewRow{
			Row: r.GetRow(), FullName: r.GetFullName(), Role: r.GetRole(), Company: r.GetCompany(), Email: r.GetEmail(),
			Relationship: r.GetRelationship(), RelationshipGuessed: r.GetRelationshipGuessed(),
			Duplicate: r.GetDuplicate(), DuplicateContactId: r.GetDuplicateContactId(), DuplicateReason: r.GetDuplicateReason(),
			RepeatsRow: r.GetRepeatsRow(), Problem: r.GetProblem(),
		})
	}
	return out
}

func contactsToAPI(contacts []*kagamiv1.Contact) []*apiv1.Contact {
	out := make([]*apiv1.Contact, 0, len(contacts))
	for _, c := range contacts {
		out = append(out, contactToAPI(c))
	}
	return out
}

func importReportToAPI(r *kagamiv1.ImportReport) *apiv1.ImportReport {
	rowErrors := make([]*apiv1.ImportRowError, 0, len(r.GetErrors()))
	for _, e := range r.GetErrors() {
		rowErrors = append(rowErrors, &apiv1.ImportRowError{Row: e.GetRow(), Column: e.GetColumn(), Message: e.GetMessage()})
	}
	return &apiv1.ImportReport{
		RowsTotal:   r.GetRowsTotal(),
		RowsCreated: r.GetRowsCreated(),
		RowsUpdated: r.GetRowsUpdated(),
		RowsFailed:  r.GetRowsFailed(),
		RowsSkipped: r.GetRowsSkipped(),
		Errors:      rowErrors,
	}
}
