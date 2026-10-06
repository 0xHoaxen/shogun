package app

import (
	"errors"
	"fmt"
	"strings"

	"github.com/0xHoaxen/shogun/pkg/llm"
	"github.com/0xHoaxen/shogun/services/fude/internal/domain"
)

const (
	subjectPrefix = "subject:"

	systemPrompt = `You draft messages for one person, in their own voice. Write as them, in the first person.
Use only facts given to you. Never invent employers, dates, numbers or shared history.
Reply with the draft only, no preface and no commentary.`

	emailFormat = "Format: the first line is `Subject: <subject>`, then a blank line, then the body."
	plainFormat = "Format: the body only, no subject line."
)

// ErrEmptyDraft means the model answered with no body.
var ErrEmptyDraft = errors.New("app: model returned an empty draft")

// defaultInstructions is what a kind is asked to do when the owner has not
// written a template for it.
var defaultInstructions = map[domain.Kind]string{
	domain.KindCoverLetter: "Write a short cover letter for the role below. Lead with why this company, then one or two relevant strengths.",
	domain.KindOutreach:    "Write a short, warm first message to the contact below. Be specific, ask for one small thing.",
	domain.KindFollowUp:    "Write a brief, polite follow-up to a message that has had no reply. Do not guilt the reader.",
	domain.KindPost:        "Write a short post about what the owner learned, in a plain and honest tone.",
	domain.KindOneOff:      "Write the message the owner asks for below.",
}

// promptInput is everything the prompt is built from.
type promptInput struct {
	Kind         domain.Kind
	Channel      domain.Channel
	Instructions string
	Target       string
	ExtraContext string
	Voice        []string
}

// featureFor returns the pkg/llm feature a kind is metered under. Follow-ups
// and one-offs are outreach-shaped and share its budget.
func featureFor(k domain.Kind) string {
	switch k {
	case domain.KindCoverLetter:
		return "fude.cover_letter"
	case domain.KindPost:
		return "fude.post"
	default:
		return "fude.outreach"
	}
}

// buildPrompt returns the request for one generation. The voice samples go in
// the system prompt, which the API caches across calls.
func buildPrompt(in promptInput) llm.Request {
	system := systemPrompt
	if len(in.Voice) > 0 {
		system += "\n\nExamples of how this person writes:\n" + strings.Join(wrapSamples(in.Voice), "\n")
	}

	format := plainFormat
	if in.Channel == domain.ChannelEmail {
		format = emailFormat
	}
	parts := []string{in.Instructions, format}
	if in.Target != "" {
		parts = append(parts, "About:\n"+in.Target)
	}
	if in.ExtraContext != "" {
		parts = append(parts, "Also keep in mind:\n"+in.ExtraContext)
	}
	return llm.Request{
		System:   system,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: strings.Join(parts, "\n\n")}},
	}
}

func wrapSamples(samples []string) []string {
	out := make([]string, len(samples))
	for i, s := range samples {
		out[i] = fmt.Sprintf("<example>\n%s\n</example>", s)
	}
	return out
}

// parseOutput splits the model's answer into subject and body. Only email
// drafts have a subject.
func parseOutput(channel domain.Channel, text string) (subject, body string, err error) {
	text = strings.TrimSpace(text)
	if channel == domain.ChannelEmail && len(text) >= len(subjectPrefix) &&
		strings.EqualFold(text[:len(subjectPrefix)], subjectPrefix) {
		line, rest, _ := strings.Cut(text, "\n")
		subject = strings.TrimSpace(line[len(subjectPrefix):])
		text = strings.TrimSpace(rest)
	}
	if text == "" {
		return "", "", ErrEmptyDraft
	}
	return subject, text, nil
}
