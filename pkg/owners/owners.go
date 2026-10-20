// Package owners keeps, in each service's own schema, the small list of users
// the service needs to run per-user schedules: who is active and which time
// zone their day is counted in. torii owns the users; this list is filled from
// its user.* events, so a service never calls torii to ask.
package owners

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	_ "github.com/0xHoaxen/shogun/pkg/schedule" // embeds the time zone database Location needs
)

// Statuses of an owner. Deleted is kept as a tombstone, so an event that
// arrives late cannot bring the owner back.
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
	StatusDeleted  = "deleted"
)

// DefaultTimezone is the zone used when an owner's own is not known.
const DefaultTimezone = "Asia/Kolkata"

// ErrNotFound is returned by Get for an owner the service has not been told about.
var ErrNotFound = errors.New("owners: not found")

//go:embed sql/00001_owners.sql
var sqlFS embed.FS

// SQL is a filesystem whose root holds 00001_owners.sql, the goose-format
// definition of the owners table. Services copy the snippet into a migration;
// tests apply it directly with postgres.Migrate.
var SQL = mustSub(sqlFS, "sql")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err) // dir is a compile-time constant that always exists
	}
	return sub
}

// Owner is one user as a service sees them.
type Owner struct {
	ID       uuid.UUID
	Timezone string
	Status   string
	Version  int32
}

// Location is the owner's time zone, or DefaultTimezone (then UTC) when theirs
// does not load.
func (o Owner) Location() *time.Location {
	for _, name := range []string{o.Timezone, DefaultTimezone} {
		if loc, err := time.LoadLocation(name); err == nil {
			return loc
		}
	}
	return time.UTC
}

// LocalDate is the calendar date at now in the owner's zone, as YYYY-MM-DD.
func (o Owner) LocalDate(now time.Time) string {
	return now.In(o.Location()).Format(time.DateOnly)
}

// ReachedLocalTime reports whether the owner's clock at now is at or past
// hour:minute of their day.
func (o Owner) ReachedLocalTime(now time.Time, hour, minute int) bool {
	local := now.In(o.Location())
	return local.Hour()*60+local.Minute() >= hour*60+minute
}

// DBTX is a pool or a transaction.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

const upsertSQL = `INSERT INTO owners (id, timezone, status, version)
VALUES ($1, $2, $3, $4)
ON CONFLICT (id) DO UPDATE
SET timezone = EXCLUDED.timezone, status = EXCLUDED.status, version = EXCLUDED.version, updated_at = now()
WHERE owners.version < EXCLUDED.version`

// Upsert stores o. It changes nothing when the table already holds the same or
// a newer version, so events can arrive twice or out of order.
func Upsert(ctx context.Context, db DBTX, o Owner) error {
	_, err := upsert(ctx, db, o)
	return err
}

// upsert is Upsert that also reports whether o was newer than what was held.
func upsert(ctx context.Context, db DBTX, o Owner) (applied bool, err error) {
	if err := o.validate(); err != nil {
		return false, err
	}
	tag, err := db.Exec(ctx, upsertSQL, o.ID, o.Timezone, o.Status, o.Version)
	if err != nil {
		return false, fmt.Errorf("owners: upsert: %w", err)
	}
	return tag.RowsAffected() > 0, nil
}

func (o Owner) validate() error {
	switch {
	case o.ID == uuid.Nil:
		return errors.New("owners: id is required")
	case o.Timezone == "":
		return errors.New("owners: timezone is required")
	case o.Status != StatusActive && o.Status != StatusDisabled && o.Status != StatusDeleted:
		return fmt.Errorf("owners: unknown status %q", o.Status)
	}
	return nil
}

// ListActive returns every active owner, in a stable order.
func ListActive(ctx context.Context, db DBTX) ([]Owner, error) {
	return query(ctx, db, `SELECT id, timezone, status, version FROM owners WHERE status = 'active' ORDER BY id`)
}

// Get returns the owner, or ErrNotFound.
func Get(ctx context.Context, db DBTX, id uuid.UUID) (Owner, error) {
	found, err := query(ctx, db, `SELECT id, timezone, status, version FROM owners WHERE id = $1`, id)
	if err != nil {
		return Owner{}, err
	}
	if len(found) == 0 {
		return Owner{}, ErrNotFound
	}
	return found[0], nil
}

func query(ctx context.Context, db DBTX, sql string, args ...any) ([]Owner, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("owners: query: %w", err)
	}
	defer rows.Close()
	var out []Owner
	for rows.Next() {
		var o Owner
		if err := rows.Scan(&o.ID, &o.Timezone, &o.Status, &o.Version); err != nil {
			return nil, fmt.Errorf("owners: scan: %w", err)
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("owners: read: %w", err)
	}
	return out, nil
}
