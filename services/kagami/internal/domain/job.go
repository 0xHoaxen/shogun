package domain

import "time"

// JobStatus is a stage in the job pipeline. Values match the jobs.status CHECK.
type JobStatus string

// The job pipeline stages, in the order a role normally moves through them.
const (
	JobSaved       JobStatus = "saved"
	JobApplied     JobStatus = "applied"
	JobShortlisted JobStatus = "shortlisted"
	JobInterview   JobStatus = "interview"
	JobOffer       JobStatus = "offer"
	JobRejected    JobStatus = "rejected"
)

// jobTransitions follows docs/shogun-state-machines.md.
var jobTransitions = transitions[JobStatus]{
	JobSaved:       {JobApplied, JobRejected},
	JobApplied:     {JobShortlisted, JobInterview, JobRejected, JobOffer},
	JobShortlisted: {JobInterview, JobRejected, JobOffer},
	JobInterview:   {JobInterview, JobOffer, JobRejected}, // interview again is the next round
	JobOffer:       {JobRejected},                         // declined
	JobRejected:    {JobApplied},                          // reopened
}

// Valid reports whether s is a known job status.
func (s JobStatus) Valid() bool {
	_, ok := jobTransitions[s]
	return ok
}

// CanMoveTo reports whether a job in status s may move to next.
func (s JobStatus) CanMoveTo(next JobStatus) bool {
	return jobTransitions.allows(s, next)
}

// Job is a role being tracked. Methods return a changed copy.
type Job struct {
	ID           string
	CompanyID    string
	Title        string
	Status       JobStatus
	AppliedOn    *time.Time
	NextFollowUp *time.Time
	Version      int32
	UpdatedAt    time.Time
}

// ChangeStatus returns the job moved to next, or a *TransitionError.
func (j Job) ChangeStatus(next JobStatus, now time.Time) (Job, error) {
	if !j.Status.CanMoveTo(next) {
		return j, &TransitionError{
			Reason: ReasonJobStatusInvalidTransition,
			From:   string(j.Status),
			To:     string(next),
		}
	}
	j.Status = next
	j.UpdatedAt = now
	return j, nil
}
