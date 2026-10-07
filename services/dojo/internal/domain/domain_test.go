package domain_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/services/dojo/internal/domain"
)

var today = time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)

func TestItemStatusTransitions(t *testing.T) {
	all := []domain.ItemStatus{domain.StatusPlanned, domain.StatusInProgress, domain.StatusDone}
	allowed := map[domain.ItemStatus][]domain.ItemStatus{
		domain.StatusPlanned:    {domain.StatusInProgress, domain.StatusDone},
		domain.StatusInProgress: {domain.StatusPlanned, domain.StatusDone},
		domain.StatusDone:       {domain.StatusInProgress},
	}
	for _, from := range all {
		for _, to := range all {
			want := slices.Contains(allowed[from], to)
			if got := from.CanMoveTo(to); got != want {
				t.Errorf("%s -> %s: got %v, want %v", from, to, got, want)
			}
		}
	}
}

func TestChangeStatusStampsDates(t *testing.T) {
	earlier := today.AddDate(0, 0, -3)
	tests := []struct {
		name        string
		item        domain.Item
		to          domain.ItemStatus
		wantStarted *time.Time
		wantDone    *time.Time
	}{
		{"start stamps the start", domain.Item{Status: domain.StatusPlanned}, domain.StatusInProgress, &today, nil},
		{"finish stamps the end", domain.Item{Status: domain.StatusInProgress, StartedOn: &earlier}, domain.StatusDone, &earlier, &today},
		{"reopen keeps the start and clears the end", domain.Item{Status: domain.StatusDone, StartedOn: &earlier, CompletedOn: &today}, domain.StatusInProgress, &earlier, nil},
		{"back to planned clears both", domain.Item{Status: domain.StatusInProgress, StartedOn: &earlier}, domain.StatusPlanned, nil, nil},
		{"finish without starting leaves the start empty", domain.Item{Status: domain.StatusPlanned}, domain.StatusDone, nil, &today},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.item.ChangeStatus(tt.to, today)
			if err != nil {
				t.Fatalf("ChangeStatus: %v", err)
			}
			if got.Status != tt.to || !sameDate(got.StartedOn, tt.wantStarted) || !sameDate(got.CompletedOn, tt.wantDone) {
				t.Fatalf("got %+v, want started %v done %v", got, tt.wantStarted, tt.wantDone)
			}
		})
	}
}

func TestChangeStatusRefusesADisallowedMove(t *testing.T) {
	item := domain.Item{Status: domain.StatusDone, CompletedOn: &today}

	got, err := item.ChangeStatus(domain.StatusPlanned, today)

	var terr *domain.TransitionError
	if !errors.As(err, &terr) || terr.Reason != domain.ReasonItemStatusInvalidTransition {
		t.Fatalf("err = %v, want a TransitionError with the stable reason", err)
	}
	if got.Status != domain.StatusDone {
		t.Fatalf("status = %s, want it unchanged", got.Status)
	}
}

func TestItemInputValidate(t *testing.T) {
	long := make([]byte, 201)
	for i := range long {
		long[i] = 'a'
	}
	tests := []struct {
		name    string
		in      domain.ItemInput
		wantErr bool
	}{
		{"good", domain.ItemInput{Title: " Go course ", Kind: domain.KindCourse, URL: "https://go.dev/learn"}, false},
		{"no url is fine", domain.ItemInput{Title: "x", Kind: domain.KindBook}, false},
		{"empty title", domain.ItemInput{Title: "  ", Kind: domain.KindBook}, true},
		{"long title", domain.ItemInput{Title: string(long), Kind: domain.KindBook}, true},
		{"unknown kind", domain.ItemInput{Title: "x", Kind: "podcast"}, true},
		{"javascript url", domain.ItemInput{Title: "x", Kind: domain.KindBook, URL: "javascript:alert(1)"}, true},
		{"url without host", domain.ItemInput{Title: "x", Kind: domain.KindBook, URL: "https://"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.in.Validate()

			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, domain.ErrInvalid) {
				t.Fatalf("err = %v, want it to wrap ErrInvalid", err)
			}
			if err == nil && got.Title != "Go course" && tt.name == "good" {
				t.Fatalf("title = %q, want it trimmed", got.Title)
			}
		})
	}
}

func TestActivityInputValidate(t *testing.T) {
	tests := []struct {
		name     string
		in       domain.ActivityInput
		wantErr  bool
		wantTags []string
		wantOn   time.Time
	}{
		{"defaults the date to today", domain.ActivityInput{Summary: "read ch. 3"}, false, []string{}, today},
		{"cleans tags", domain.ActivityInput{Summary: "x", Tags: []string{" Go ", "go", "", "SQL"}}, false, []string{"go", "sql"}, today},
		{"keeps a past date", domain.ActivityInput{Summary: "x", OccurredOn: today.AddDate(0, 0, -1)}, false, []string{}, today.AddDate(0, 0, -1)},
		{"empty summary", domain.ActivityInput{Summary: " "}, true, nil, today},
		{"future date", domain.ActivityInput{Summary: "x", OccurredOn: today.AddDate(0, 0, 1)}, true, nil, today},
		{"negative minutes", domain.ActivityInput{Summary: "x", Minutes: -1}, true, nil, today},
		{"too many minutes", domain.ActivityInput{Summary: "x", Minutes: 24*60 + 1}, true, nil, today},
		{"too many tags", domain.ActivityInput{Summary: "x", Tags: []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j", "k"}}, true, nil, today},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.in.Validate(today)

			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if !slices.Equal(got.Tags, tt.wantTags) || !got.OccurredOn.Equal(tt.wantOn) {
				t.Fatalf("got tags %v on %v, want %v on %v", got.Tags, got.OccurredOn, tt.wantTags, tt.wantOn)
			}
		})
	}
}

func sameDate(a, b *time.Time) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.Equal(*b)
}
