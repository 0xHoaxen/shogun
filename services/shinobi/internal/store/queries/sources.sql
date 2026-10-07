-- name: InsertSource :one
INSERT INTO sources (id, owner_id, name, kind, config, schedule, enabled)
VALUES (@id, @owner_id, @name, @kind, @config, @schedule, @enabled)
RETURNING *;

-- name: GetSource :one
SELECT * FROM sources WHERE owner_id = @owner_id AND id = @id;

-- name: ListSources :many
SELECT * FROM sources WHERE owner_id = @owner_id ORDER BY name, id;

-- name: UpdateSource :one
-- The kind is fixed once a source exists; changing what a source reads would
-- leave its old postings under the wrong one.
UPDATE sources SET name = @name, config = @config, schedule = @schedule, enabled = @enabled
WHERE owner_id = @owner_id AND id = @id
RETURNING *;

-- name: ListEnabledSources :many
-- Every owner's enabled sources, for the scheduler.
SELECT * FROM sources WHERE enabled ORDER BY owner_id, id;

-- name: MarkSourceRun :exec
UPDATE sources SET last_run_at = @last_run_at, last_error = @last_error
WHERE id = @id;
