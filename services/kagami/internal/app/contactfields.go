package app

import (
	"net/mail"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/kagami/internal/store/db"
)

// Accepted values of contacts.relationship and contacts.preferred_channel.
var (
	contactRelationships = []string{"recruiter", "engineer", "manager", "alumni", "friend", "other"}
	contactChannels      = []string{"email", "linkedin", "x", "phone", "other"}
)

// ContactFields carries the editable values of a contact as they arrive from a
// request or a CSV row. Dates are YYYY-MM-DD; ids are UUIDs; an empty
// optional value means none.
type ContactFields struct {
	FullName         string
	CompanyID        string
	Role             string
	Email            string
	LinkedinURL      string
	XHandle          string
	Phone            string
	Relationship     string
	HowWeMet         string
	PreferredChannel string
	LastContacted    string
	NextFollowUp     string
	TargetRole       string
	JobID            string
	Tags             []string
	Notes            string
}

// contactValues is ContactFields after validation, ready for the store.
type contactValues struct {
	FullName         string
	CompanyID        *uuid.UUID
	Role             *string
	Email            *string
	LinkedinURL      *string
	XHandle          *string
	Phone            *string
	Relationship     *string
	HowWeMet         *string
	PreferredChannel *string
	LastContacted    *time.Time
	NextFollowUp     *time.Time
	TargetRole       *string
	JobID            *uuid.UUID
	Tags             []string
	Notes            *string
}

// parseContactFields validates f and converts it to store values. Text is
// trimmed; relationship and channel are lower-cased; tags are lower-cased,
// de-duplicated and sorted.
func parseContactFields(f ContactFields) (contactValues, error) {
	v := contactValues{FullName: strings.TrimSpace(f.FullName)}
	if v.FullName == "" {
		return v, invalid("FULL_NAME_REQUIRED", "full_name is required")
	}
	var err error
	if v.CompanyID, err = optionalID("company_id", f.CompanyID); err != nil {
		return v, err
	}
	if v.JobID, err = optionalID("job_id", f.JobID); err != nil {
		return v, err
	}
	if v.LastContacted, err = parseDate("last_contacted", f.LastContacted); err != nil {
		return v, err
	}
	if v.NextFollowUp, err = parseDate("next_follow_up", f.NextFollowUp); err != nil {
		return v, err
	}
	if v.Email, err = parseEmail(f.Email); err != nil {
		return v, err
	}
	if v.LinkedinURL, err = parseWebURL("linkedin_url", f.LinkedinURL); err != nil {
		return v, err
	}
	if v.Relationship, err = parseChoice("relationship", f.Relationship, contactRelationships); err != nil {
		return v, err
	}
	if v.PreferredChannel, err = parseChoice("preferred_channel", f.PreferredChannel, contactChannels); err != nil {
		return v, err
	}
	v.Role = trimmedPtr(f.Role)
	v.XHandle = trimmedPtr(f.XHandle)
	v.Phone = trimmedPtr(f.Phone)
	v.HowWeMet = trimmedPtr(f.HowWeMet)
	v.TargetRole = trimmedPtr(f.TargetRole)
	v.Notes = trimmedPtr(f.Notes)
	v.Tags = normalizeTags(f.Tags)
	return v, nil
}

func trimmedPtr(s string) *string {
	return strPtr(strings.TrimSpace(s))
}

func optionalID(field, value string) (*uuid.UUID, error) {
	if value == "" {
		return nil, nil
	}
	id, err := parseID(field, value)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func parseEmail(value string) (*string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	addr, err := mail.ParseAddress(value)
	if err != nil || addr.Address != value {
		return nil, invalid("INVALID_EMAIL", "email is not a plain address")
	}
	return &value, nil
}

func parseWebURL(field, value string) (*string, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	u, err := url.Parse(value)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, invalid("INVALID_URL", "%s must be an http or https URL", field)
	}
	return &value, nil
}

func parseChoice(field, value string, allowed []string) (*string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return nil, nil
	}
	if !slices.Contains(allowed, value) {
		return nil, invalid("INVALID_"+strings.ToUpper(field), "%s must be one of %v", field, allowed)
	}
	return &value, nil
}

func normalizeTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !slices.Contains(out, t) {
			out = append(out, t)
		}
	}
	slices.Sort(out)
	return out
}

// toContactFields renders a stored contact back to ContactFields.
func toContactFields(c db.Contact) ContactFields {
	f := ContactFields{
		FullName: c.FullName, Role: deref(c.Role), Email: deref(c.Email), LinkedinURL: deref(c.LinkedinUrl),
		XHandle: deref(c.XHandle), Phone: deref(c.Phone), Relationship: deref(c.Relationship),
		HowWeMet: deref(c.HowWeMet), PreferredChannel: deref(c.PreferredChannel),
		TargetRole: deref(c.TargetRole), Notes: deref(c.Notes), Tags: slices.Clone(c.Tags),
		LastContacted: formatDate(c.LastContacted), NextFollowUp: formatDate(c.NextFollowUp),
	}
	if c.CompanyID != nil {
		f.CompanyID = c.CompanyID.String()
	}
	if c.JobID != nil {
		f.JobID = c.JobID.String()
	}
	return f
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func formatDate(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(dateLayout)
}

// overlayContactFields returns base with the masked fields taken from req.
// Neither argument is modified.
func overlayContactFields(base ContactFields, paths []string, req ContactFields) (ContactFields, error) {
	if len(paths) == 0 {
		return base, invalid("UPDATE_MASK_REQUIRED", "update_mask must name at least one field")
	}
	for _, path := range paths {
		switch path {
		case "full_name":
			base.FullName = req.FullName
		case "company_id":
			base.CompanyID = req.CompanyID
		case "role":
			base.Role = req.Role
		case "email":
			base.Email = req.Email
		case "linkedin_url":
			base.LinkedinURL = req.LinkedinURL
		case "x_handle":
			base.XHandle = req.XHandle
		case "phone":
			base.Phone = req.Phone
		case "relationship":
			base.Relationship = req.Relationship
		case "how_we_met":
			base.HowWeMet = req.HowWeMet
		case "preferred_channel":
			base.PreferredChannel = req.PreferredChannel
		case "last_contacted":
			base.LastContacted = req.LastContacted
		case "next_follow_up":
			base.NextFollowUp = req.NextFollowUp
		case "target_role":
			base.TargetRole = req.TargetRole
		case "job_id":
			base.JobID = req.JobID
		case "tags":
			base.Tags = slices.Clone(req.Tags)
		case "notes":
			base.Notes = req.Notes
		case "status":
			return base, invalid("STATUS_NOT_EDITABLE", "status changes go through ChangeContactStatus")
		default:
			return base, invalid("UNKNOWN_FIELD", "%q cannot be updated", path)
		}
	}
	return base, nil
}
