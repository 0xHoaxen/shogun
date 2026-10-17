package store

// PurgeStatements remove everything one user owns, each run with the owner id
// as $1. This includes the sealed GitHub token.
var PurgeStatements = []string{
	`DELETE FROM suggestions WHERE owner_id = $1`,
	`DELETE FROM github_snapshots WHERE owner_id = $1`,
	`DELETE FROM github_accounts WHERE owner_id = $1`,
}
