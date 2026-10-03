package app

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/0xHoaxen/shogun/services/kagami/internal/domain"
)

const importHeader = "full_name,company,role,email,linkedin_url,x_handle,phone,relationship,how_we_met," +
	"status,preferred_channel,last_contacted,next_follow_up,target_role,job_link,tags,notes\n"

func TestParseImportCSVReadsRows(t *testing.T) {
	data := "\xEF\xBB\xBF" + strings.ToUpper(importHeader[:9]) + importHeader[9:] +
		"Priya Raman,Lumen,Staff Engineer,priya@lumen.example,,,,recruiter,meetup,Replied,email,2026-09-29,,,,go; referral,Met at GopherCon\n" +
		"Arjun Mehta,Corvid,,,,,,,,,,,,,,,\n"

	rows, err := parseImportCSV([]byte(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rows) != 2 || rows[0].number != 2 || rows[1].number != 3 {
		t.Fatalf("got %d rows, numbered %v", len(rows), rows)
	}
	first := rows[0]
	if first.problem != nil || first.fields.FullName != "Priya Raman" || first.company != "Lumen" ||
		first.status != domain.ContactReplied || first.fields.Notes != "Met at GopherCon" {
		t.Fatalf("got %+v", first)
	}
	if !slices.Equal(first.fields.Tags, []string{"go", " referral"}) {
		t.Fatalf("got tags %q", first.fields.Tags)
	}
	if rows[1].status != "" || rows[1].fields.Tags != nil {
		t.Fatalf("empty cells must stay empty: %+v", rows[1])
	}
}

func TestParseImportCSVKeepsRowProblemsOnTheRow(t *testing.T) {
	data := importHeader +
		"Short,row\n" +
		"Kenji,Northwind,,,,,,,,ghosted,,,,,,,\n" +
		"Mira,Corvid,,,,,,,,,,,,,,,\n"

	rows, err := parseImportCSV([]byte(data))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d rows, want 3", len(rows))
	}
	if rows[0].problem == nil || rows[0].problem.Reason != "INVALID_ROW" {
		t.Fatalf("short row: %+v", rows[0].problem)
	}
	if rows[1].problem == nil || rows[1].problem.Field != "status" {
		t.Fatalf("bad status: %+v", rows[1].problem)
	}
	if rows[2].problem != nil {
		t.Fatalf("good row got a problem: %+v", rows[2].problem)
	}
}

func TestParseImportCSVRejectsTheFile(t *testing.T) {
	tests := []struct {
		name       string
		data       string
		wantReason string
	}{
		{"empty", "", "IMPORT_EMPTY"},
		{"missing column", "full_name,company\n", "INVALID_HEADER"},
		{"columns out of order", strings.Replace(importHeader, "full_name,company", "company,full_name", 1), "INVALID_HEADER"},
		{"broken quoting", importHeader + "\"Priya,Lumen\n", "INVALID_CSV"},
		{"too big", importHeader + strings.Repeat("x", maxImportBytes), "IMPORT_TOO_LARGE"},
		{"too many rows", importHeader + strings.Repeat("A,,,,,,,,,,,,,,,,\n", maxImportRows+1), "IMPORT_TOO_LARGE"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := parseImportCSV([]byte(tt.data))
			var ia *InvalidArgumentError
			if !errors.As(err, &ia) || ia.Reason != tt.wantReason {
				t.Fatalf("got %v, want reason %s", err, tt.wantReason)
			}
		})
	}
}

func TestParseImportCSVAcceptsAHeaderOnlyFile(t *testing.T) {
	rows, err := parseImportCSV([]byte(importHeader))
	if err != nil || len(rows) != 0 {
		t.Fatalf("got %d rows, %v", len(rows), err)
	}
}
