package store

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Page sizes for list calls.
const (
	DefaultPageSize = 50
	MaxPageSize     = 200
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

// cursor is the keyset position after the last row of a page.
type cursor struct {
	UpdatedAt time.Time `json:"t"`
	ID        uuid.UUID `json:"id"`
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
	if err := json.Unmarshal(raw, &c); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidPageToken, err)
	}
	return &c, nil
}

// afterParams splits a cursor into the nullable pair the list queries take.
func afterParams(c *cursor) (*time.Time, *uuid.UUID) {
	if c == nil {
		return nil, nil
	}
	return &c.UpdatedAt, &c.ID
}

// trimPage drops the look-ahead row fetched to learn whether another page
// exists, and returns the token for that page.
func trimPage[T any](rows []T, size int32, key func(T) (time.Time, uuid.UUID)) ([]T, string) {
	if int32(len(rows)) <= size {
		return rows, ""
	}
	rows = rows[:size]
	updatedAt, id := key(rows[size-1])
	return rows, cursor{UpdatedAt: updatedAt, ID: id}.encode()
}
