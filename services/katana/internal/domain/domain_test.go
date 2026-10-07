package domain_test

import (
	"errors"
	"testing"

	"github.com/0xHoaxen/shogun/services/katana/internal/domain"
)

func TestDecideAllowsOneDecisionOnAnOpenSuggestion(t *testing.T) {
	states := []domain.State{domain.StateOpen, domain.StateAccepted, domain.StateDismissed}
	for _, from := range states {
		for _, to := range states {
			want := from == domain.StateOpen && to != domain.StateOpen

			got, err := from.Decide(to)

			if (err == nil) != want {
				t.Errorf("%s -> %s: err = %v, want allowed %v", from, to, err, want)
			}
			var derr *domain.DecisionError
			if err != nil && (!errors.As(err, &derr) || derr.Reason != domain.ReasonSuggestionAlreadyDecided || got != from) {
				t.Errorf("%s -> %s: err %v, state %s; want the stable reason and no change", from, to, err, got)
			}
			if err == nil && got != to {
				t.Errorf("%s -> %s: got %s", from, to, got)
			}
		}
	}
}

func TestSuggestionValidate(t *testing.T) {
	good := domain.Suggestion{
		Target: domain.TargetResume, Section: domain.SectionProjects, After: " Built a worker pool ", Reason: "merged 3 PRs",
		Evidence: []domain.Evidence{{Label: "PR 12", URL: "https://github.com/o/r/pull/12"}},
	}
	with := func(f func(*domain.Suggestion)) domain.Suggestion { s := good; f(&s); return s }
	tests := []struct {
		name    string
		in      domain.Suggestion
		wantErr bool
	}{
		{"good", good, false},
		{"no evidence is fine", with(func(s *domain.Suggestion) { s.Evidence = nil }), false},
		{"unknown target", with(func(s *domain.Suggestion) { s.Target = "cv" }), true},
		{"unknown section", with(func(s *domain.Suggestion) { s.Section = "hobbies" }), true},
		{"no after", with(func(s *domain.Suggestion) { s.After = " " }), true},
		{"no reason", with(func(s *domain.Suggestion) { s.Reason = "" }), true},
		{"javascript evidence", with(func(s *domain.Suggestion) { s.Evidence = []domain.Evidence{{Label: "x", URL: "javascript:1"}} }), true},
		{"evidence without a label", with(func(s *domain.Suggestion) { s.Evidence = []domain.Evidence{{URL: "https://example.com"}} }), true},
		{"too much evidence", with(func(s *domain.Suggestion) {
			s.Evidence = make([]domain.Evidence, 11)
			for i := range s.Evidence {
				s.Evidence[i] = domain.Evidence{Label: "x", URL: "https://example.com"}
			}
		}), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.in.Validate()

			if (err != nil) != tt.wantErr || (err != nil && !errors.Is(err, domain.ErrInvalid)) {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && tt.name == "good" && got.After != "Built a worker pool" {
				t.Fatalf("after = %q, want it trimmed", got.After)
			}
		})
	}
}
