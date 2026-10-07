package fetch

import "testing"

func TestRobotsRules(t *testing.T) {
	const file = `
# a comment
User-agent: *
Disallow: /private
Disallow: /tmp/
Allow: /private/open

User-agent: shogun-shinobi
Disallow: /jobs/secret
Allow: /jobs
`
	tests := []struct {
		name string
		body string
		path string
		want bool
	}{
		{"named group wins over wildcard", file, "/private/area", true},
		{"named group disallow", file, "/jobs/secret/1", false},
		{"named group allow, longer than nothing", file, "/jobs/1", true},
		{"no rules at all allows", "", "/anything", true},
		{"empty disallow allows", "User-agent: *\nDisallow:\n", "/x", true},
		{"wildcard disallow prefix", "User-agent: *\nDisallow: /private\n", "/private/x", false},
		{"wildcard other path", "User-agent: *\nDisallow: /private\n", "/public", true},
		{"allow beats disallow on a longer match", "User-agent: *\nDisallow: /a\nAllow: /a/b\n", "/a/b/c", true},
		{"allow beats disallow on a tie", "User-agent: *\nDisallow: /a\nAllow: /a\n", "/a", true},
		{"disallow everything", "User-agent: *\nDisallow: /\n", "/feed.xml", false},
		{"root path", "User-agent: *\nDisallow: /\n", "", false},
		{"group ends at the next user-agent block", "User-agent: other\nDisallow: /\n\nUser-agent: *\nDisallow: /x\n", "/y", true},
		{"consecutive user-agents share rules", "User-agent: other\nUser-agent: *\nDisallow: /x\n", "/x", false},
		{"case insensitive keys", "USER-AGENT: *\nDISALLOW: /x\n", "/x/y", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseRobots([]byte(tt.body), "shogun-shinobi").allowed(tt.path); got != tt.want {
				t.Fatalf("allowed(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
