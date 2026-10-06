package app

import (
	"errors"
	"strings"
	"testing"

	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
)

func TestParseOutput(t *testing.T) {
	tests := []struct {
		name        string
		channel     domain.Channel
		text        string
		wantSubject string
		wantBody    string
		wantErr     error
	}{
		{"email with subject", domain.ChannelEmail, "Subject: Hello Lumen\n\nDear team,\nHi.", "Hello Lumen", "Dear team,\nHi.", nil},
		{"subject prefix is case-insensitive", domain.ChannelEmail, "SUBJECT:  Hi \nBody", "Hi", "Body", nil},
		{"email without a subject line", domain.ChannelEmail, "Just a body", "", "Just a body", nil},
		{"linkedin keeps a subject-looking line", domain.ChannelLinkedIn, "Subject: not a subject\nmore", "", "Subject: not a subject\nmore", nil},
		{"surrounding space is trimmed", domain.ChannelX, "\n  post \n", "", "post", nil},
		{"subject only is empty", domain.ChannelEmail, "Subject: Hi", "", "", ErrEmptyDraft},
		{"blank answer is empty", domain.ChannelEmail, "  \n ", "", "", ErrEmptyDraft},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			subject, body, err := parseOutput(tt.channel, tt.text)

			if !errors.Is(err, tt.wantErr) || subject != tt.wantSubject || body != tt.wantBody {
				t.Fatalf("got (%q, %q, %v), want (%q, %q, %v)", subject, body, err, tt.wantSubject, tt.wantBody, tt.wantErr)
			}
		})
	}
}

func TestBuildPrompt(t *testing.T) {
	req := buildPrompt(promptInput{
		Kind: domain.KindCoverLetter, Channel: domain.ChannelEmail, Instructions: "Write it.",
		Target: "Backend role at Lumen", ExtraContext: "mention Go", Voice: []string{"sample one", "sample two"},
	})

	user := req.Messages[0].Content
	for _, want := range []string{"Write it.", "Subject:", "Backend role at Lumen", "mention Go"} {
		if !strings.Contains(user, want) {
			t.Errorf("user message is missing %q:\n%s", want, user)
		}
	}
	for _, want := range []string{"<example>\nsample one\n</example>", "sample two"} {
		if !strings.Contains(req.System, want) {
			t.Errorf("system prompt is missing %q", want)
		}
	}
	if req.Cache {
		t.Error("a draft must not be served from the response cache")
	}
}

func TestBuildPromptWithoutOptionalParts(t *testing.T) {
	req := buildPrompt(promptInput{Kind: domain.KindPost, Channel: domain.ChannelLinkedIn, Instructions: "Post."})

	if strings.Contains(req.System, "Examples") || strings.Contains(req.Messages[0].Content, "About:") ||
		strings.Contains(req.Messages[0].Content, "Subject:") {
		t.Fatalf("unexpected sections:\n%s\n%s", req.System, req.Messages[0].Content)
	}
}

func TestFeatureFor(t *testing.T) {
	want := map[domain.Kind]string{
		domain.KindCoverLetter: "fude.cover_letter", domain.KindOutreach: "fude.outreach",
		domain.KindFollowUp: "fude.outreach", domain.KindOneOff: "fude.outreach", domain.KindPost: "fude.post",
	}
	for kind, feature := range want {
		if got := featureFor(kind); got != feature {
			t.Errorf("featureFor(%s) = %s, want %s", kind, got, feature)
		}
		if defaultInstructions[kind] == "" {
			t.Errorf("no default instructions for %s", kind)
		}
	}
}
