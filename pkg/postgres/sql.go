package postgres

import (
	"embed"
	"io/fs"
)

//go:embed sql/00001_outbox_inbox.sql
var sqlFS embed.FS

// OutboxInboxSQL is a filesystem whose root holds 00001_outbox_inbox.sql, the
// goose-format outbox and inbox table definitions. Services copy the snippet
// into their first migration; tests apply it directly with Migrate.
var OutboxInboxSQL = mustSub(sqlFS, "sql")

func mustSub(fsys fs.FS, dir string) fs.FS {
	sub, err := fs.Sub(fsys, dir)
	if err != nil {
		panic(err) // dir is a compile-time constant that always exists
	}
	return sub
}
