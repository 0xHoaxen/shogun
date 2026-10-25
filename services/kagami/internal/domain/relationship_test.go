package domain_test

import (
	"testing"

	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
)

func TestGuessRelationshipFromTheJobTitle(t *testing.T) {
	tests := []struct {
		role        string
		want        string
		wantGuessed bool
	}{
		{"Technical Recruiter", "recruiter", true},
		{"Senior Recruiting Partner", "recruiter", true},
		{"Talent Acquisition Lead", "recruiter", true},
		{"Sr. Talent Partner", "recruiter", true},
		{"Sourcer", "recruiter", true},
		{"Sourcing Specialist", "recruiter", true},
		{"Engineering Manager", "manager", true},
		{"Head of Talent", "recruiter", true},
		{"Head of Platform", "manager", true},
		{"Director of Engineering", "manager", true},
		{"VP, Engineering", "manager", true},
		{"vp engineering", "manager", true},
		{"Team Lead", "manager", true},
		{"CTO", "manager", true},
		{"Product Manager", "manager", true},
		{"Backend Engineer", "engineer", true},
		{"Software Developer", "engineer", true},
		{"Staff SWE", "engineer", true},
		{"Dev Advocate", "engineer", true},
		{"Platform Engineering", "engineer", true},
		{"Designer", "other", true},
		{"Founder", "other", true},
		{"", "other", false},
		{"   ", "other", false},
		{"---", "other", false},
		// Whole words only: these contain a keyword without being one.
		{"Ahead of the curve", "other", true},
		{"Leader", "other", true},
		{"Developing markets analyst", "other", true},
		{"Devin Smith", "other", true},
		{"Victor Patel, VPN support", "other", true},
	}
	for _, tt := range tests {
		t.Run(tt.role, func(t *testing.T) {
			got, guessed := domain.GuessRelationship(tt.role)

			if got != tt.want || guessed != tt.wantGuessed {
				t.Fatalf("GuessRelationship(%q) = %q, %v; want %q, %v", tt.role, got, guessed, tt.want, tt.wantGuessed)
			}
		})
	}
}

func TestNormalizeNameIgnoresCaseAndExtraSpace(t *testing.T) {
	for _, name := range []string{"Priya Raman", "  priya   RAMAN ", "PRIYA\tRaman"} {
		if got := domain.NormalizeName(name); got != "priya raman" {
			t.Errorf("NormalizeName(%q) = %q, want priya raman", name, got)
		}
	}
	if domain.NormalizeName("   ") != "" {
		t.Error("a blank name should normalise to nothing")
	}
}
