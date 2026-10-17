package store

// PurgeStatements remove everything one user owns, each run with the owner id
// as $1.
var PurgeStatements = []string{
	`DELETE FROM notifications WHERE owner_id = $1`,
	`DELETE FROM channel_settings WHERE owner_id = $1`,
}
