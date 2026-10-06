package gmail

import (
	"errors"
	"io"
	"mime"
	netmail "net/mail"
	"strings"
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail"
)

var testNow = time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC)

func TestBuildMessageProducesMailThatParsesBack(t *testing.T) {
	raw, err := buildMessage(mail.Outgoing{
		From: "me@example.com", To: []string{"jobs@lumen.example", "hr@lumen.example"},
		Subject: "Hello, Lumen ✓", Body: "Dear team,\nI would like to help.\n", DraftID: "draft-1",
	}, testNow)
	if err != nil {
		t.Fatalf("build: %v", err)
	}

	msg, err := netmail.ReadMessage(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	body, _ := io.ReadAll(msg.Body)

	if got := msg.Header.Get(mail.DraftHeader); got != "draft-1" {
		t.Errorf("draft header = %q", got)
	}
	if got := msg.Header.Get("To"); got != "jobs@lumen.example, hr@lumen.example" {
		t.Errorf("To = %q", got)
	}
	if subj, _ := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject")); subj != "Hello, Lumen ✓" {
		t.Errorf("Subject = %q", subj)
	}
	if !strings.Contains(string(body), "I would like to help.") {
		t.Errorf("body = %q", body)
	}
}

func TestBuildMessageWithoutADraftHasNoDraftHeader(t *testing.T) {
	raw, err := buildMessage(mail.Outgoing{From: "me@example.com", To: []string{"a@b.example"}, Subject: "s", Body: "b"}, testNow)

	if err != nil || strings.Contains(string(raw), mail.DraftHeader) {
		t.Fatalf("err %v, raw:\n%s", err, raw)
	}
}

func TestBuildMessageRefusesHeaderInjectionAndBadAddresses(t *testing.T) {
	ok := mail.Outgoing{From: "me@example.com", To: []string{"a@b.example"}, Subject: "s", Body: "b"}
	tests := []struct {
		name   string
		mutate func(m *mail.Outgoing)
	}{
		{"line break in subject", func(m *mail.Outgoing) { m.Subject = "hi\r\nBcc: evil@x.example" }},
		{"line break in draft id", func(m *mail.Outgoing) { m.DraftID = "d\nBcc: evil@x.example" }},
		{"line break in recipient", func(m *mail.Outgoing) { m.To = []string{"a@b.example\r\nBcc: evil@x.example"} }},
		{"line break in sender", func(m *mail.Outgoing) { m.From = "me@example.com\nBcc: evil@x.example" }},
		{"display name", func(m *mail.Outgoing) { m.To = []string{"Bob <b@c.example>"} }},
		{"two addresses in one", func(m *mail.Outgoing) { m.To = []string{"a@b.example, c@d.example"} }},
		{"not an address", func(m *mail.Outgoing) { m.To = []string{"nobody"} }},
		{"no recipient", func(m *mail.Outgoing) { m.To = nil }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := ok
			m.To = append([]string(nil), ok.To...)
			tt.mutate(&m)

			raw, err := buildMessage(m, testNow)

			if !errors.Is(err, ErrInvalidMessage) || raw != nil {
				t.Fatalf("got %q, %v; want ErrInvalidMessage", raw, err)
			}
		})
	}
}
