-- name: InsertActivity :one
INSERT INTO activities (id, owner_id, item_id, summary, minutes, occurred_on, tags, created_at)
VALUES (@id, @owner_id, @item_id, @summary, @minutes, @occurred_on, @tags, @created_at)
RETURNING *;

-- name: GetActivity :one
SELECT * FROM activities WHERE owner_id = @owner_id AND id = @id;

-- name: ListActivities :many
-- Keyset pagination on (created_at, id), newest first. A NULL item_id means
-- every activity.
SELECT * FROM activities
WHERE owner_id = @owner_id
  AND (sqlc.narg(item_id)::uuid IS NULL OR item_id = sqlc.narg(item_id)::uuid)
  AND (
    sqlc.narg(after_created_at)::timestamptz IS NULL
    OR (created_at, id) < (sqlc.narg(after_created_at)::timestamptz, sqlc.narg(after_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;
