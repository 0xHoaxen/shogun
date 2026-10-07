package domain

import (
	"errors"
	"fmt"
	"time"
)

// Errors of reading GitHub, which the app layer maps to answers for the owner.
var (
	// ErrNotModified means GitHub had nothing new since the ETag sent.
	ErrNotModified = errors.New("github: not modified")
	// ErrNotConfigured means no GitHub user or token is set.
	ErrNotConfigured = errors.New("github: user or token not configured")
	// ErrUnauthorized means GitHub refused the token.
	ErrUnauthorized = errors.New("github: token refused")
	// ErrUnavailable means GitHub could not be reached or failed.
	ErrUnavailable = errors.New("github: unavailable")
)

// RateLimitError means GitHub's rate limit is used up until ResetAt.
type RateLimitError struct {
	ResetAt time.Time
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("github: rate limited until %s", e.ResetAt.UTC().Format(time.RFC3339))
}

// Repo is one of the owner's own repositories.
type Repo struct {
	Name     string    `json:"name"`
	URL      string    `json:"url"`
	Language string    `json:"language"`
	Stars    int       `json:"stars"`
	PushedAt time.Time `json:"pushed_at"`
}

// PullRequest is a merged pull request the owner wrote.
type PullRequest struct {
	Repo     string    `json:"repo"`
	Title    string    `json:"title"`
	URL      string    `json:"url"`
	MergedAt time.Time `json:"merged_at"`
}

// Contributions is what the owner has contributed.
type Contributions struct {
	MergedPRs []PullRequest `json:"merged_prs"`
}

// Snapshot is what GitHub showed about the owner at one time.
type Snapshot struct {
	Repos         []Repo
	Contributions Contributions
	// ETag validates Repos on the next read.
	ETag string
}
