-- name: InsertSuggestion :one
INSERT INTO suggestions (id, owner_id, target, section, before, after, reason, evidence, created_at)
VALUES (@id, @owner_id, @target, @section, @before, @after, @reason, @evidence, @created_at)
RETURNING *;

-- name: GetSuggestion :one
SELECT * FROM suggestions WHERE owner_id = @owner_id AND id = @id;

-- name: ListSuggestions :many
-- Keyset pagination on (created_at, id), newest first. A NULL after_* pair
-- means the first page; a NULL state or target means any.
SELECT * FROM suggestions
WHERE owner_id = @owner_id
  AND (sqlc.narg(state)::text IS NULL OR state = sqlc.narg(state)::text)
  AND (sqlc.narg(target)::text IS NULL OR target = sqlc.narg(target)::text)
  AND (
    sqlc.narg(after_created_at)::timestamptz IS NULL
    OR (created_at, id) < (sqlc.narg(after_created_at)::timestamptz, sqlc.narg(after_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;

-- name: DecideSuggestion :one
-- Returns no row unless the suggestion is open, so a decision is made once.
UPDATE suggestions SET state = @state, decided_at = @decided_at
WHERE owner_id = @owner_id AND id = @id AND state = 'open'
RETURNING *;
