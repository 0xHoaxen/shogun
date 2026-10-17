package store

// PurgeStatements remove everything one user owns, each run with the owner id
// as $1.
var PurgeStatements = []string{
	`DELETE FROM daily_rollups WHERE owner_id = $1`,
	`DELETE FROM facts WHERE owner_id = $1`,
}
