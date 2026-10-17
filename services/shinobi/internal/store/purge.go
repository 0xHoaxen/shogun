package store

// PurgeStatements remove everything one user owns, each run with the owner id
// as $1. Children come first so foreign keys hold; scores have no owner_id of
// their own and are reached through their posting.
var PurgeStatements = []string{
	`DELETE FROM scores WHERE posting_id IN (SELECT id FROM postings WHERE owner_id = $1)`,
	`DELETE FROM postings WHERE owner_id = $1`,
	`DELETE FROM sources WHERE owner_id = $1`,
	`DELETE FROM preferences WHERE owner_id = $1`,
}
