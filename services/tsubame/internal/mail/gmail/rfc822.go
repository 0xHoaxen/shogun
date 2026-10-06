package gmail

import (
	"bytes"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	netmail "net/mail"
	"strings"
	"time"

	"github.com/0xHoaxen/shogun/services/tsubame/internal/mail"
)

// ErrInvalidMessage means an outgoing message cannot be turned into mail, for
// example because a header value holds a line break.
var ErrInvalidMessage = errors.New("gmail: invalid outgoing message")

// buildMessage returns the RFC 822 text of m. Values that could end a header
// early (CR or LF) are refused rather than escaped, so a draft can never add a
// header of its own.
func buildMessage(m mail.Outgoing, now time.Time) ([]byte, error) {
	if len(m.To) == 0 {
		return nil, fmt.Errorf("%w: no recipient", ErrInvalidMessage)
	}
	from, err := plainAddress(m.From)
	if err != nil {
		return nil, fmt.Errorf("%w: from", err)
	}
	to := make([]string, len(m.To))
	for i, a := range m.To {
		if to[i], err = plainAddress(a); err != nil {
			return nil, fmt.Errorf("%w: to", err)
		}
	}
	if hasBreak(m.Subject) || hasBreak(m.DraftID) {
		return nil, fmt.Errorf("%w: header value holds a line break", ErrInvalidMessage)
	}

	var b bytes.Buffer
	header := func(name, value string) { fmt.Fprintf(&b, "%s: %s\r\n", name, value) }
	header("From", from)
	header("To", strings.Join(to, ", "))
	header("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	header("Date", now.UTC().Format(time.RFC1123Z))
	header("MIME-Version", "1.0")
	header("Content-Type", `text/plain; charset="UTF-8"`)
	header("Content-Transfer-Encoding", "quoted-printable")
	if m.DraftID != "" {
		header(mail.DraftHeader, m.DraftID)
	}
	b.WriteString("\r\n")

	w := quotedprintable.NewWriter(&b)
	if _, err := w.Write([]byte(m.Body)); err != nil {
		return nil, fmt.Errorf("encode body: %w", err)
	}
	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("encode body: %w", err)
	}
	return b.Bytes(), nil
}

// plainAddress returns addr as a bare address, refusing display names, groups
// and line breaks.
func plainAddress(addr string) (string, error) {
	parsed, err := netmail.ParseAddress(strings.TrimSpace(addr))
	if err != nil || parsed.Name != "" || hasBreak(addr) {
		return "", ErrInvalidMessage
	}
	return parsed.Address, nil
}

func hasBreak(s string) bool { return strings.ContainsAny(s, "\r\n") }
