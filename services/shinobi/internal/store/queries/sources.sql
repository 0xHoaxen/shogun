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
-- owner-scope: internal the scheduler scans every active owner's sources; each run is then queued for its own owner
-- Every active owner's enabled sources with that owner's time zone, for the
-- scheduler. A disabled or deleted owner's sources are not read.
SELECT s.*, o.timezone AS owner_timezone
FROM sources s JOIN owners o ON o.id = s.owner_id
WHERE s.enabled AND o.status = 'active'
ORDER BY s.owner_id, s.id;

-- name: MarkSourceRun :exec
-- owner-scope: child source already loaded with the owner by RunSourceFor
UPDATE sources SET last_run_at = @last_run_at, last_error = @last_error
WHERE id = @id;

-- name: DeleteSource :one
-- Removes a source with its postings and their scores in one statement; the
-- foreign keys are checked once it ends. No row means the owner has no such
-- source.
WITH doomed AS (
    SELECT p.id FROM postings p WHERE p.owner_id = @owner_id AND p.source_id = @id
), gone_scores AS (
    DELETE FROM scores WHERE posting_id IN (SELECT doomed.id FROM doomed)
), gone_postings AS (
    DELETE FROM postings WHERE postings.id IN (SELECT doomed.id FROM doomed)
)
DELETE FROM sources WHERE sources.owner_id = @owner_id AND sources.id = @id
RETURNING sources.id;
