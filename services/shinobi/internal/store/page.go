package store

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

// Page sizes for list calls.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200

	// unscored is the score an unscored posting sorts by: below every real one.
	unscored float32 = -1
)

// Page selects one page of a list. The zero value is the first page of the
// default size.
type Page struct {
	Size  int32
	Token string
}

func (p Page) size() int32 {
	switch {
	case p.Size <= 0:
		return DefaultPageSize
	case p.Size > MaxPageSize:
		return MaxPageSize
	default:
		return p.Size
	}
}

// cursor is the keyset position after the last posting of a page.
type cursor struct {
	Score float32   `json:"s"`
	ID    uuid.UUID `json:"id"`
}

func (c cursor) encode() string {
	raw, _ := json.Marshal(c) // two plain fields cannot fail to marshal
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeCursor reads a token from a previous page. An empty token means the
// first page and returns nil.
func decodeCursor(token string) (*cursor, error) {
	if token == "" {
		return nil, nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPageToken, err)
	}
	var c cursor
	if err := json.Unmarshal(raw, &c); err != nil || c.ID == uuid.Nil {
		return nil, fmt.Errorf("%w: unreadable cursor", ErrInvalidPageToken)
	}
	return &c, nil
}
