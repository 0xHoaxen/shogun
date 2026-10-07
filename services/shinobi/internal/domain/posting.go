package domain

import (
	"net/url"
	"strings"
	"time"
)

// Limits on posting fields.
const (
	maxExternalIDLen = 500
	maxTitleLen      = 300
	maxCompanyLen    = 200
	maxLocationLen   = 200
	maxDescription   = 20000
)

// Candidate is a posting as a source listed it, before it is stored.
type Candidate struct {
	ExternalID  string
	Title       string
	Company     string
	URL         string
	Location    string
	PostedAt    *time.Time
	Description string
}

// Validate returns the candidate trimmed, or an error wrapping ErrInvalid. A
// link that is not http or https is cleared rather than kept, since it is
// shown to the owner as a link. Over-long text is cut, not refused, because a
// source's data is not the owner's to fix.
func (c Candidate) Validate() (Candidate, error) {
	c.ExternalID, c.Title = strings.TrimSpace(c.ExternalID), strings.TrimSpace(c.Title)
	switch {
	case c.ExternalID == "":
		return c, invalid("posting has no id")
	case len(c.ExternalID) > maxExternalIDLen:
		return c, invalid("posting id is longer than %d bytes", maxExternalIDLen)
	case c.Title == "":
		return c, invalid("posting has no title")
	}
	c.Title = clip(c.Title, maxTitleLen)
	c.Company = clip(strings.TrimSpace(c.Company), maxCompanyLen)
	c.Location = clip(strings.TrimSpace(c.Location), maxLocationLen)
	c.Description = clip(strings.TrimSpace(c.Description), maxDescription)
	c.URL = strings.TrimSpace(c.URL)
	if u, err := url.Parse(c.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || len(c.URL) > maxURLLen {
		c.URL = ""
	}
	return c, nil
}

// clip shortens s to at most n bytes without splitting a character.
func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !isRuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }
