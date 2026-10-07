package domain_test

import (
	"errors"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/services/shinobi/internal/domain"
)

func TestSourceInputValidate(t *testing.T) {
	mapping := &domain.FieldMapping{ID: "id", Title: "title", ItemsPath: "data.jobs"}
	rss := domain.SourceInput{Name: " Feed ", Kind: domain.KindRSS, Config: domain.SourceConfig{URL: "https://jobs.example.com/feed.xml"}}
	api := domain.SourceInput{Name: "API", Kind: domain.KindAPI, Config: domain.SourceConfig{URL: "https://jobs.example.com/api", Mapping: mapping}}
	file := domain.SourceInput{Name: "File", Kind: domain.KindFile, Config: domain.SourceConfig{Document: `[{"id":"1","title":"x"}]`, Mapping: mapping}}
	with := func(in domain.SourceInput, f func(*domain.SourceInput)) domain.SourceInput { f(&in); return in }
	tests := []struct {
		name    string
		in      domain.SourceInput
		wantErr bool
	}{
		{"rss", rss, false},
		{"api", api, false},
		{"file", file, false},
		{"no name", with(rss, func(s *domain.SourceInput) { s.Name = " " }), true},
		{"unknown kind", with(rss, func(s *domain.SourceInput) { s.Kind = "html" }), true},
		{"http url", with(rss, func(s *domain.SourceInput) { s.Config.URL = "http://jobs.example.com/feed" }), true},
		{"no url", with(rss, func(s *domain.SourceInput) { s.Config.URL = "" }), true},
		{"credentials in url", with(rss, func(s *domain.SourceInput) { s.Config.URL = "https://u:p@jobs.example.com/" }), true},
		{"localhost", with(rss, func(s *domain.SourceInput) { s.Config.URL = "https://localhost/feed" }), true},
		{"internal name", with(rss, func(s *domain.SourceInput) { s.Config.URL = "https://db.internal/feed" }), true},
		{"loopback ip", with(rss, func(s *domain.SourceInput) { s.Config.URL = "https://127.0.0.1/feed" }), true},
		{"private ip", with(rss, func(s *domain.SourceInput) { s.Config.URL = "https://10.0.0.5/feed" }), true},
		{"metadata ip", with(rss, func(s *domain.SourceInput) { s.Config.URL = "https://169.254.169.254/latest" }), true},
		{"ipv6 loopback", with(rss, func(s *domain.SourceInput) { s.Config.URL = "https://[::1]/feed" }), true},
		{"public ip", with(rss, func(s *domain.SourceInput) { s.Config.URL = "https://93.184.216.34/feed" }), false},
		{"api without mapping", with(api, func(s *domain.SourceInput) { s.Config.Mapping = nil }), true},
		{"mapping without title", with(api, func(s *domain.SourceInput) { s.Config.Mapping = &domain.FieldMapping{ID: "id"} }), true},
		{"file without document", with(file, func(s *domain.SourceInput) { s.Config.Document = "" }), true},
		{"file too big", with(file, func(s *domain.SourceInput) { s.Config.Document = strings.Repeat("x", 1<<20+1) }), true},
		{"bad schedule", with(rss, func(s *domain.SourceInput) { s.Schedule = "every day" }), true},
		{"six field schedule", with(rss, func(s *domain.SourceInput) { s.Schedule = "0 0 7 * * *" }), true},
		{"good schedule", with(rss, func(s *domain.SourceInput) { s.Schedule = "30 6 * * 1-5" }), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.in.Validate()

			if (err != nil) != tt.wantErr || (err != nil && !errors.Is(err, domain.ErrInvalid)) {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got.Schedule == "" {
				t.Fatal("schedule was not defaulted")
			}
		})
	}
}

func TestSourceInputValidateCleansWhatTheKindDoesNotUse(t *testing.T) {
	in := domain.SourceInput{
		Name: " Feed ", Kind: domain.KindRSS,
		Config: domain.SourceConfig{URL: " https://jobs.example.com/feed ", Document: "stale", Mapping: &domain.FieldMapping{ID: "x"}},
	}

	got, err := in.Validate()

	if err != nil || got.Name != "Feed" || got.Config.URL != "https://jobs.example.com/feed" || got.Config.Document != "" ||
		got.Config.Mapping != nil || got.Schedule != domain.DefaultSchedule {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestIsPublicIP(t *testing.T) {
	tests := map[string]bool{
		"93.184.216.34": true, "8.8.8.8": true, "2606:4700:4700::1111": true,
		"127.0.0.1": false, "10.1.2.3": false, "172.16.0.1": false, "192.168.1.1": false, "169.254.169.254": false,
		"100.64.0.1": false, "0.0.0.0": false, "224.0.0.1": false, "::1": false, "fe80::1": false, "fc00::1": false,
		"::ffff:127.0.0.1": false, "::ffff:10.0.0.1": false,
	}
	for raw, want := range tests {
		if got := domain.IsPublicIP(netip.MustParseAddr(raw)); got != want {
			t.Errorf("IsPublicIP(%s) = %v, want %v", raw, got, want)
		}
	}
}

func TestPreferencesValidate(t *testing.T) {
	got, err := domain.Preferences{
		Roles: []string{" Backend Engineer ", "backend engineer", ""}, Locations: []string{"Remote"}, MinScore: 0.6,
	}.Validate()
	if err != nil || !slices.Equal(got.Roles, []string{"backend engineer"}) || !slices.Equal(got.Locations, []string{"remote"}) {
		t.Fatalf("got %+v, %v", got, err)
	}

	many := make([]string, 31)
	for i := range many {
		many[i] = string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	bad := []domain.Preferences{
		{MinScore: -0.1},
		{MinScore: 1.1},
		{Roles: many, MinScore: 0.5},
		{Exclude: []string{strings.Repeat("x", 61)}, MinScore: 0.5},
	}
	for i, p := range bad {
		if _, err := p.Validate(); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("case %d: err = %v, want ErrInvalid", i, err)
		}
	}
	if !domain.DefaultPreferences().Empty() || domain.DefaultPreferences().MinScore != domain.DefaultMinScore {
		t.Error("default preferences should be empty with the default minimum")
	}
}

func TestCandidateValidate(t *testing.T) {
	good := domain.Candidate{ExternalID: " 42 ", Title: " Backend Engineer ", Company: "Acme", URL: "https://acme.example/jobs/42", Location: "Remote"}

	got, err := good.Validate()
	if err != nil || got.ExternalID != "42" || got.Title != "Backend Engineer" || got.URL != good.URL {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, c := range []domain.Candidate{{Title: "x"}, {ExternalID: "1"}, {ExternalID: strings.Repeat("x", 501), Title: "x"}} {
		if _, err := c.Validate(); !errors.Is(err, domain.ErrInvalid) {
			t.Errorf("%+v: err = %v, want ErrInvalid", c, err)
		}
	}
	for _, link := range []string{"javascript:alert(1)", "ftp://x.example/a", "/relative", "https://"} {
		c := good
		c.URL = link
		if got, err := c.Validate(); err != nil || got.URL != "" {
			t.Errorf("link %q: got %q, %v; want it cleared", link, got.URL, err)
		}
	}
	long := good
	long.Title = strings.Repeat("é", 200) // 400 bytes
	if got, err := long.Validate(); err != nil || len(got.Title) > 300 || !strings.HasSuffix(got.Title, "é") {
		t.Errorf("long title: len %d, %v; want a cut at a character boundary", len(got.Title), err)
	}
}

func prefs() domain.Preferences {
	return domain.Preferences{
		Roles: []string{"backend engineer", "platform engineer"}, Locations: []string{"remote", "bangalore"},
		MustHave: []string{"go", "postgres"}, NiceToHave: []string{"kubernetes", "grpc"}, Exclude: []string{"unpaid"}, MinScore: 0.7,
	}
}

func TestRuleScores(t *testing.T) {
	tests := []struct {
		name  string
		c     domain.Candidate
		want  float32
		wants []string
	}{
		{
			"everything",
			domain.Candidate{Title: "Senior Backend Engineer", Location: "Remote", Description: "Go, Postgres, Kubernetes and gRPC."},
			1,
			[]string{"wanted role backend engineer", "location matches remote", "every required term", "2 of 2"},
		},
		{
			"role and location only",
			domain.Candidate{Title: "Backend Engineer", Location: "Bangalore"},
			0.55,
			[]string{"lacks go, postgres", "0 of 2"},
		},
		{
			"half the required terms",
			domain.Candidate{Title: "Platform Engineer", Location: "Remote", Description: "We use Go."},
			0.35 + 0.2 + 0.15,
			[]string{"lacks postgres"},
		},
		{
			"wrong role and place",
			domain.Candidate{Title: "Sales Manager", Location: "Berlin", Description: "Go Postgres kubernetes grpc"},
			0.45,
			[]string{"none of the wanted roles", "none of the wanted ones"},
		},
		{
			"excluded",
			domain.Candidate{Title: "Backend Engineer", Location: "Remote", Description: "Unpaid internship. Go Postgres"},
			0,
			[]string{"excluded term unpaid"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := domain.Rule(prefs(), tt.c)

			if got.Value != tt.want {
				t.Fatalf("score = %v, want %v (%v)", got.Value, tt.want, got.Reasons)
			}
			all := strings.Join(got.Reasons, "; ")
			for _, w := range tt.wants {
				if !strings.Contains(all, w) {
					t.Errorf("reasons %q lack %q", all, w)
				}
			}
		})
	}
}

func TestRuleMatchesWholeWordsOnly(t *testing.T) {
	p := domain.Preferences{MustHave: []string{"go", "c++"}, MinScore: 0.5}

	google := domain.Rule(p, domain.Candidate{Title: "Engineer at Google", Description: "Mongo and django"})
	genuine := domain.Rule(p, domain.Candidate{Title: "Engineer", Description: "Go or C++ required"})

	if google.Value >= genuine.Value || genuine.Value != 1 {
		t.Fatalf("google %v, genuine %v; want substrings inside other words not to count", google.Value, genuine.Value)
	}
}

func TestRuleWithNoPreferencesMatchesNothing(t *testing.T) {
	got := domain.Rule(domain.DefaultPreferences(), domain.Candidate{Title: "Anything"})

	if got.Value != 0 || len(got.Reasons) != 1 {
		t.Fatalf("got %+v", got)
	}
}

func TestRuleIgnoresPartsThePreferencesLeaveOut(t *testing.T) {
	got := domain.Rule(domain.Preferences{Roles: []string{"engineer"}, MinScore: 0.5}, domain.Candidate{Title: "Engineer"})

	if got.Value != 1 {
		t.Fatalf("score = %v, want 1: unset parts earn full credit", got.Value)
	}
}

func TestClassifyBands(t *testing.T) {
	p := domain.Preferences{MinScore: 0.7}
	tests := []struct {
		score float32
		want  domain.Outcome
	}{
		{1, domain.Match}, {0.7, domain.Match}, {0.69, domain.Borderline}, {0.5, domain.Borderline}, {0.49, domain.Reject}, {0, domain.Reject},
	}
	for _, tt := range tests {
		if got := p.Classify(tt.score); got != tt.want {
			t.Errorf("Classify(%v) = %v, want %v", tt.score, got, tt.want)
		}
	}
}

func TestDue(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Fatal(err)
	}
	at := func(h, m int) time.Time { return time.Date(2026, 10, 7, h, m, 0, 0, ist) }
	last := at(7, 0)
	tests := []struct {
		name     string
		schedule string
		lastRun  *time.Time
		now      time.Time
		wantDue  bool
		wantSlot string
	}{
		{"never run", "0 7 * * *", nil, at(3, 0), true, domain.FirstRunSlot},
		{"just ran", "0 7 * * *", &last, at(7, 1), false, ""},
		{"not yet tomorrow", "0 7 * * *", &last, at(23, 59), false, ""},
		{"due tomorrow at seven", "0 7 * * *", &last, time.Date(2026, 10, 8, 7, 0, 0, 0, ist), true, "2026-10-08T01:30:00Z"},
		{"late for tomorrow", "0 7 * * *", &last, time.Date(2026, 10, 8, 12, 0, 0, 0, ist), true, "2026-10-08T01:30:00Z"},
		{"hourly", "0 * * * *", &last, at(8, 0), true, "2026-10-07T02:30:00Z"},
		{"weekdays only", "0 7 * * 1-5", &last, time.Date(2026, 10, 10, 8, 0, 0, 0, ist), true, "2026-10-08T01:30:00Z"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			slot, due, err := domain.Due(tt.schedule, tt.lastRun, tt.now, ist)

			if err != nil || due != tt.wantDue || slot != tt.wantSlot {
				t.Fatalf("slot %q due %v err %v; want %q %v", slot, due, err, tt.wantSlot, tt.wantDue)
			}
		})
	}
	if _, _, err := domain.Due("nonsense", nil, time.Now(), ist); !errors.Is(err, domain.ErrInvalid) {
		t.Fatalf("bad schedule err = %v", err)
	}
}
