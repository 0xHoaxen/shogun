package store

// PurgeStatements remove everything one user owns, each run with the owner id
// as $1. Children come first so foreign keys hold; the event tables have no
// owner_id of their own and are reached through their parent.
var PurgeStatements = []string{
	`DELETE FROM contact_events WHERE contact_id IN (SELECT id FROM contacts WHERE owner_id = $1)`,
	`DELETE FROM contacts WHERE owner_id = $1`,
	`DELETE FROM job_events WHERE job_id IN (SELECT id FROM jobs WHERE owner_id = $1)`,
	`DELETE FROM jobs WHERE owner_id = $1`,
	`DELETE FROM companies WHERE owner_id = $1`,
	`DELETE FROM imports WHERE owner_id = $1`,
}
