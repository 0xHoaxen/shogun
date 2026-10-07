package domain

import (
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/robfig/cron/v3"
)

// Limits on source fields.
const (
	maxNameLen     = 100
	maxURLLen      = 2000
	maxDocumentLen = 1 << 20
	maxPathLen     = 200

	// DefaultSchedule is when a source runs when the owner names no time.
	DefaultSchedule = "0 7 * * *"
)

// SourceKind is how a source is read. Values match sources.kind.
type SourceKind string

// Source kinds.
const (
	KindRSS  SourceKind = "rss"
	KindAPI  SourceKind = "api"
	KindFile SourceKind = "file"
)

// Valid reports whether k is a known kind.
func (k SourceKind) Valid() bool { return k == KindRSS || k == KindAPI || k == KindFile }

// FieldMapping says where in a JSON item each posting field is. A path is
// dot-separated keys such as "company.name". ItemsPath locates the array of
// items in the document and is empty when the document is the array.
type FieldMapping struct {
	ItemsPath   string `json:"items_path,omitempty"`
	ID          string `json:"id,omitempty"`
	Title       string `json:"title,omitempty"`
	Company     string `json:"company,omitempty"`
	URL         string `json:"url,omitempty"`
	Location    string `json:"location,omitempty"`
	PostedAt    string `json:"posted_at,omitempty"`
	Description string `json:"description,omitempty"`
}

// SourceConfig is what a source reads. Which fields matter depends on the kind.
type SourceConfig struct {
	URL      string        `json:"url,omitempty"`
	Document string        `json:"document,omitempty"`
	Mapping  *FieldMapping `json:"mapping,omitempty"`
}

// SourceInput is what the owner types for a source.
type SourceInput struct {
	Name     string
	Kind     SourceKind
	Config   SourceConfig
	Schedule string
	Enabled  bool
}

// ParseSchedule reads a five-field cron expression.
func ParseSchedule(expr string) (cron.Schedule, error) {
	s, err := cron.ParseStandard(expr)
	if err != nil {
		return nil, invalid("schedule %q is not a five-field cron expression", expr)
	}
	return s, nil
}

// Validate returns the input cleaned up, or an error wrapping ErrInvalid. An
// empty schedule becomes DefaultSchedule.
func (in SourceInput) Validate() (SourceInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	in.Schedule = strings.TrimSpace(in.Schedule)
	if in.Schedule == "" {
		in.Schedule = DefaultSchedule
	}
	switch {
	case in.Name == "":
		return in, invalid("name is required")
	case len(in.Name) > maxNameLen:
		return in, invalid("name is longer than %d bytes", maxNameLen)
	case !in.Kind.Valid():
		return in, invalid("kind %q is not known", in.Kind)
	}
	if _, err := ParseSchedule(in.Schedule); err != nil {
		return in, err
	}
	cfg, err := in.Config.validate(in.Kind)
	if err != nil {
		return in, err
	}
	in.Config = cfg
	return in, nil
}

func (c SourceConfig) validate(kind SourceKind) (SourceConfig, error) {
	c.URL, c.Document = strings.TrimSpace(c.URL), strings.TrimSpace(c.Document)
	switch kind {
	case KindRSS:
		c.Document, c.Mapping = "", nil
		if err := checkFeedURL(c.URL); err != nil {
			return c, err
		}
	case KindAPI:
		c.Document = ""
		if err := checkFeedURL(c.URL); err != nil {
			return c, err
		}
		return c.withMapping()
	case KindFile:
		c.URL = ""
		if c.Document == "" {
			return c, invalid("document is required for a file source")
		}
		if len(c.Document) > maxDocumentLen {
			return c, invalid("document is longer than %d bytes", maxDocumentLen)
		}
		return c.withMapping()
	}
	return c, nil
}

func (c SourceConfig) withMapping() (SourceConfig, error) {
	if c.Mapping == nil {
		return c, invalid("a field mapping is required")
	}
	m := c.Mapping.trimmed()
	if m.ID == "" || m.Title == "" {
		return c, invalid("the mapping must say where the id and the title are")
	}
	for _, p := range []string{m.ItemsPath, m.ID, m.Title, m.Company, m.URL, m.Location, m.PostedAt, m.Description} {
		if len(p) > maxPathLen {
			return c, invalid("a mapping path is longer than %d bytes", maxPathLen)
		}
	}
	c.Mapping = &m
	return c, nil
}

func (m FieldMapping) trimmed() FieldMapping {
	return FieldMapping{
		ItemsPath: strings.TrimSpace(m.ItemsPath), ID: strings.TrimSpace(m.ID), Title: strings.TrimSpace(m.Title),
		Company: strings.TrimSpace(m.Company), URL: strings.TrimSpace(m.URL), Location: strings.TrimSpace(m.Location),
		PostedAt: strings.TrimSpace(m.PostedAt), Description: strings.TrimSpace(m.Description),
	}
}

// checkFeedURL requires an https address that does not name this machine or a
// private network. Fetching re-checks the address it actually connects to.
func checkFeedURL(raw string) error {
	if raw == "" {
		return invalid("url is required")
	}
	if len(raw) > maxURLLen {
		return invalid("url is longer than %d bytes", maxURLLen)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil {
		return invalid("url must be an https address without credentials")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") || strings.HasSuffix(host, ".internal") {
		return invalid("url must not name this machine or a private network")
	}
	if addr, err := netip.ParseAddr(host); err == nil && !IsPublicIP(addr) {
		return invalid("url must not name this machine or a private network")
	}
	return nil
}

// cgnat is the shared address space of carrier-grade NAT, which is not public
// but is not covered by netip's IsPrivate.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// IsPublicIP reports whether addr is an address a source may be fetched from:
// not loopback, private, link-local, multicast, unspecified or shared.
func IsPublicIP(addr netip.Addr) bool {
	addr = addr.Unmap()
	return addr.IsValid() && !addr.IsPrivate() && !addr.IsLoopback() && !addr.IsLinkLocalUnicast() &&
		!addr.IsLinkLocalMulticast() && !addr.IsMulticast() && !addr.IsUnspecified() && !cgnat.Contains(addr)
}

// FirstRunSlot names the run of a source that has never run.
const FirstRunSlot = "first"

// Due says whether a source with this schedule is due at now, given when it last
// ran, and names the slot that makes it due so one slot is run once. A source
// that has never run is due at once. The schedule is read in loc.
func Due(expr string, lastRun *time.Time, now time.Time, loc *time.Location) (slot string, due bool, err error) {
	sched, err := ParseSchedule(expr)
	if err != nil {
		return "", false, err
	}
	if lastRun == nil {
		return FirstRunSlot, true, nil
	}
	next := sched.Next(lastRun.In(loc))
	if next.After(now) {
		return "", false, nil
	}
	return next.UTC().Format(time.RFC3339), true, nil
}
