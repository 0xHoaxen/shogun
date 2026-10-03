package app

import (
	"slices"
	"time"

	"github.com/0xHoaxen/shogun/services/kagami/internal/store/db"
)

// jobSources are the values of jobs.source.
var jobSources = []string{"manual", "discovery", "mail", "import"}

// JobFields carries the values an UpdateJob call may change. Dates are
// YYYY-MM-DD; an empty optional value clears the column.
type JobFields struct {
	Title        string
	URL          string
	Source       string
	Location     string
	SalaryText   string
	Description  string
	AppliedOn    string
	NextFollowUp string
}

// maskedJobUpdate is the outcome of applying a field mask to a job.
type maskedJobUpdate struct {
	params db.UpdateJobParams
	// followUpChanged is true when next_follow_up ends up different.
	followUpChanged bool
}

// applyJobMask returns the update that sets only the masked fields on current.
// current is not modified.
func applyJobMask(current db.Job, paths []string, f JobFields) (maskedJobUpdate, error) {
	if len(paths) == 0 {
		return maskedJobUpdate{}, invalid("UPDATE_MASK_REQUIRED", "update_mask must name at least one field")
	}
	p := db.UpdateJobParams{
		ID: current.ID, OwnerID: current.OwnerID, Version: current.Version,
		Title: current.Title, Url: current.Url, Source: current.Source,
		AppliedOn: current.AppliedOn, NextFollowUp: current.NextFollowUp,
		Location: current.Location, SalaryText: current.SalaryText, Description: current.Description,
	}
	for _, path := range paths {
		if err := setJobField(&p, path, f); err != nil {
			return maskedJobUpdate{}, err
		}
	}
	return maskedJobUpdate{params: p, followUpChanged: !sameDate(current.NextFollowUp, p.NextFollowUp)}, nil
}

// setJobField copies one masked field into p, which is a private copy.
func setJobField(p *db.UpdateJobParams, path string, f JobFields) error {
	var err error
	switch path {
	case "title":
		if f.Title == "" {
			return invalid("TITLE_REQUIRED", "title cannot be empty")
		}
		p.Title = f.Title
	case "source":
		if !slices.Contains(jobSources, f.Source) {
			return invalid("INVALID_SOURCE", "source must be one of %v", jobSources)
		}
		p.Source = f.Source
	case "url":
		p.Url = strPtr(f.URL)
	case "location":
		p.Location = strPtr(f.Location)
	case "salary_text":
		p.SalaryText = strPtr(f.SalaryText)
	case "description":
		p.Description = strPtr(f.Description)
	case "applied_on":
		p.AppliedOn, err = parseDate("applied_on", f.AppliedOn)
	case "next_follow_up":
		p.NextFollowUp, err = parseDate("next_follow_up", f.NextFollowUp)
	case "status":
		return invalid("STATUS_NOT_EDITABLE", "status changes go through ChangeJobStatus")
	default:
		return invalid("UNKNOWN_FIELD", "%q cannot be updated", path)
	}
	return err
}

func sameDate(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}
