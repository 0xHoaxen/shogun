package fetch

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/0xHoaxen/shogun/services/shinobi/internal/domain"
)

const (
	// MaxItems caps how many postings one read of a source takes.
	MaxItems = 500
	// maxRawBytes caps the item kept with a posting; a bigger one is not kept.
	maxRawBytes = 64 << 10
)

// ErrBadDocument means a source's answer is not the kind of document the source
// was set up to be.
var ErrBadDocument = errors.New("fetch: document cannot be read as postings")

// Item is one posting as a source listed it, with the item as received.
type Item struct {
	Candidate domain.Candidate
	// Raw is the item as JSON, kept with the posting.
	Raw []byte
}

var timeLayouts = []string{
	time.RFC3339, time.RFC1123Z, time.RFC1123, time.RFC822Z, time.RFC822, "2006-01-02T15:04:05", time.DateOnly,
}

func parseTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range timeLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

// rawOf marshals an item to keep, or a marker when it is too big.
func rawOf(v any) []byte {
	raw, err := json.Marshal(v)
	if err != nil || len(raw) > maxRawBytes {
		return []byte(`{"omitted":true}`)
	}
	return raw
}

// ParseJSON reads postings from a JSON document through a mapping. Items that
// cannot be read are skipped; the document as a whole must hold an array.
func ParseJSON(doc []byte, m domain.FieldMapping) ([]Item, error) {
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	var root any
	if err := dec.Decode(&root); err != nil {
		return nil, fmt.Errorf("%w: not JSON", ErrBadDocument)
	}
	list, ok := path(root, m.ItemsPath).([]any)
	if !ok {
		return nil, fmt.Errorf("%w: no list of items at %q", ErrBadDocument, m.ItemsPath)
	}
	items := make([]Item, 0, min(len(list), MaxItems))
	for _, entry := range list[:min(len(list), MaxItems)] {
		obj, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		items = append(items, Item{
			Raw: rawOf(obj),
			Candidate: domain.Candidate{
				ExternalID: text(path(obj, m.ID)), Title: text(path(obj, m.Title)), Company: text(path(obj, m.Company)),
				URL: text(path(obj, m.URL)), Location: text(path(obj, m.Location)),
				PostedAt: parseTime(text(path(obj, m.PostedAt))), Description: text(path(obj, m.Description)),
			},
		})
	}
	return items, nil
}

// path follows a dot-separated path of object keys. An empty path is the value
// itself, and a missing key is nil.
func path(v any, p string) any {
	if p == "" {
		return v
	}
	for _, key := range strings.Split(p, ".") {
		obj, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = obj[key]
	}
	return v
}

// text turns a scalar into text; anything else is empty.
func text(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case json.Number:
		return t.String()
	case bool:
		return strconv.FormatBool(t)
	default:
		return ""
	}
}
