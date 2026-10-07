package domain_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
)

var at = time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

func repo(name string, days int) domain.Repo {
	return domain.Repo{Name: name, URL: "https://github.com/o/" + name, PushedAt: at.AddDate(0, 0, days)}
}

func TestDiffReturnsOnlyWhatPrevLacks(t *testing.T) {
	prev := domain.Snapshot{
		Repos:         []domain.Repo{repo("a", 0)},
		Contributions: domain.Contributions{MergedPRs: []domain.PullRequest{{URL: "https://github.com/x/y/pull/1"}}},
	}
	cur := domain.Snapshot{
		Repos: []domain.Repo{repo("a", 1), repo("b", 2), repo("c", 5)},
		Contributions: domain.Contributions{MergedPRs: []domain.PullRequest{
			{URL: "https://github.com/x/y/pull/1"},
			{URL: "https://github.com/x/y/pull/2", MergedAt: at},
		}},
	}

	got := domain.Diff(&prev, cur)

	if len(got.Repos) != 2 || got.Repos[0].Name != "c" || got.Repos[1].Name != "b" {
		t.Fatalf("repos = %+v, want c then b (new, newest first)", got.Repos)
	}
	if len(got.PullRequests) != 1 || got.PullRequests[0].URL != "https://github.com/x/y/pull/2" {
		t.Fatalf("prs = %+v", got.PullRequests)
	}
}

func TestDiffWithoutAPreviousSnapshotTreatsEverythingAsNewButCapped(t *testing.T) {
	var cur domain.Snapshot
	for i := range 25 {
		cur.Repos = append(cur.Repos, repo(fmt.Sprintf("r%d", i), i))
		cur.Contributions.MergedPRs = append(cur.Contributions.MergedPRs, domain.PullRequest{URL: fmt.Sprintf("https://x/%d", i), MergedAt: at.AddDate(0, 0, i)})
	}

	got := domain.Diff(nil, cur)

	if len(got.Repos) != domain.MaxChangedRepos || got.Repos[0].Name != "r24" || len(got.PullRequests) != domain.MaxChangedPRs {
		t.Fatalf("repos %d (first %s), prs %d", len(got.Repos), got.Repos[0].Name, len(got.PullRequests))
	}
}

func TestDiffOfIdenticalSnapshotsIsEmpty(t *testing.T) {
	snap := domain.Snapshot{Repos: []domain.Repo{repo("a", 0)}}

	if got := domain.Diff(&snap, snap); !got.Empty() {
		t.Fatalf("diff = %+v, want empty", got)
	}
}
