// Package migrations embeds the goose migrations of the fude schema.
package migrations

import "embed"

// FS holds the *.sql migrations at its root, for postgres.Migrate.
//
//go:embed *.sql
var FS embed.FS
