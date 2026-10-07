package domain

import (
	"slices"
	"time"
)

// Limits on how much one suggestion run looks at, so a first sync of a large
// account stays a small prompt.
const (
	MaxChangedRepos = 10
	MaxChangedPRs   = 10
)

// Changes is what is new in a snapshot since the one before it.
type Changes struct {
	Repos        []Repo
	PullRequests []PullRequest
}

// Empty reports whether nothing is new.
func (c Changes) Empty() bool { return len(c.Repos) == 0 && len(c.PullRequests) == 0 }

// Diff returns the repositories and merged pull requests in cur that prev does
// not have, newest first and capped. A nil prev means every one is new.
func Diff(prev *Snapshot, cur Snapshot) Changes {
	knownRepos := map[string]bool{}
	knownPRs := map[string]bool{}
	if prev != nil {
		for _, r := range prev.Repos {
			knownRepos[r.URL] = true
		}
		for _, p := range prev.Contributions.MergedPRs {
			knownPRs[p.URL] = true
		}
	}
	var c Changes
	for _, r := range cur.Repos {
		if !knownRepos[r.URL] {
			c.Repos = append(c.Repos, r)
		}
	}
	for _, p := range cur.Contributions.MergedPRs {
		if !knownPRs[p.URL] {
			c.PullRequests = append(c.PullRequests, p)
		}
	}
	slices.SortFunc(c.Repos, func(a, b Repo) int { return b.PushedAt.Compare(a.PushedAt) })
	slices.SortFunc(c.PullRequests, func(a, b PullRequest) int { return b.MergedAt.Compare(a.MergedAt) })
	c.Repos = c.Repos[:min(len(c.Repos), MaxChangedRepos)]
	c.PullRequests = c.PullRequests[:min(len(c.PullRequests), MaxChangedPRs)]
	return c
}

// LearnedItem is something the owner finished learning, from dojo.
type LearnedItem struct {
	ItemID string `json:"item_id"`
	Title  string `json:"title"`
	Kind   string `json:"kind"`
	// URL is the item's link, when it has one.
	URL string `json:"url,omitempty"`
}

// Month formats t for the prompt as YYYY-MM.
func Month(t time.Time) string { return t.UTC().Format("2006-01") }
