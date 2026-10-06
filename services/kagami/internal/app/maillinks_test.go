package app

import (
	"slices"
	"testing"
)

func TestSenderAddress(t *testing.T) {
	tests := []struct{ in, want string }{
		{"hr@lumen.example", "hr@lumen.example"},
		{"Recruiter <HR@Lumen.example>", "hr@lumen.example"},
		{"  hr@lumen.example ", "hr@lumen.example"},
		{"not an address", ""},
		{"", ""},
	}
	for _, tt := range tests {
		if got := senderAddress(tt.in); got != tt.want {
			t.Errorf("senderAddress(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestCompanyDomains(t *testing.T) {
	tests := []struct {
		addr string
		want []string
	}{
		{"hr@lumen.example", []string{"lumen.example"}},
		{"hr@jobs.eu.lumen.example", []string{"jobs.eu.lumen.example", "eu.lumen.example", "lumen.example"}},
		{"someone@gmail.com", nil},
		{"someone@mail.yahoo.com", []string{"mail.yahoo.com"}},
		{"", nil},
		{"nobody", nil},
	}
	for _, tt := range tests {
		if got := companyDomains(tt.addr); !slices.Equal(got, tt.want) {
			t.Errorf("companyDomains(%q) = %v, want %v", tt.addr, got, tt.want)
		}
	}
}
