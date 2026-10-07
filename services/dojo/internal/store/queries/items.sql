-- name: InsertItem :one
INSERT INTO items (id, owner_id, title, kind, url, insight, created_at, updated_at)
VALUES (@id, @owner_id, @title, @kind, @url, @insight, @now, @now)
RETURNING *;

-- name: GetItem :one
SELECT * FROM items WHERE owner_id = @owner_id AND id = @id AND archived_at IS NULL;

-- name: ListItems :many
-- Keyset pagination on (created_at, id), newest first. A NULL after_* pair
-- means the first page; a NULL status means every status.
SELECT * FROM items
WHERE owner_id = @owner_id AND archived_at IS NULL
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  AND (
    sqlc.narg(after_created_at)::timestamptz IS NULL
    OR (created_at, id) < (sqlc.narg(after_created_at)::timestamptz, sqlc.narg(after_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;

-- name: UpdateItem :one
-- Writes every editable column; the caller applies the field mask to the
-- current row first. Returns no row when the version is stale.
UPDATE items SET
    title = @title,
    kind = @kind,
    url = @url,
    insight = @insight,
    version = version + 1,
    updated_at = @now
WHERE id = @id AND owner_id = @owner_id AND version = @version AND archived_at IS NULL
RETURNING *;

-- name: UpdateItemStatus :one
UPDATE items SET
    status = @status,
    started_on = @started_on,
    completed_on = @completed_on,
    version = version + 1,
    updated_at = @now
WHERE id = @id AND owner_id = @owner_id AND version = @version AND archived_at IS NULL
RETURNING *;
