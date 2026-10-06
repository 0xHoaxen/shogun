-- name: InsertDraft :one
INSERT INTO drafts (
    id, owner_id, kind, target_type, target_id, channel, recipient, idempotency_key
) VALUES (
    @id, @owner_id, @kind, @target_type, @target_id, @channel, @recipient, @idempotency_key
)
RETURNING *;

-- name: GetDraft :one
SELECT * FROM drafts WHERE id = @id AND owner_id = @owner_id;

-- name: GetDraftByIdempotencyKey :one
SELECT * FROM drafts WHERE owner_id = @owner_id AND idempotency_key = @idempotency_key::text;

-- name: ListDrafts :many
-- Keyset pagination on (updated_at, id), newest first. A NULL after_* pair
-- means the first page.
SELECT * FROM drafts
WHERE owner_id = @owner_id
  AND state = @state::text
  AND (
    sqlc.narg(after_updated_at)::timestamptz IS NULL
    OR (updated_at, id) < (sqlc.narg(after_updated_at)::timestamptz, sqlc.narg(after_id)::uuid)
  )
ORDER BY updated_at DESC, id DESC
LIMIT @row_limit;

-- name: UpdateDraftState :one
-- Writes the state, newest version and failure reason. Returns no row when the
-- version is stale.
UPDATE drafts SET
    state = @state,
    current_version = @current_version,
    failure_reason = @failure_reason,
    version = version + 1,
    updated_at = now()
WHERE id = @id AND owner_id = @owner_id AND version = @version
RETURNING *;

-- name: SetDraftRecipient :execrows
-- Fills in the recipient of a draft that has none; a recipient the owner gave
-- is never replaced.
UPDATE drafts SET recipient = @recipient
WHERE id = @id AND owner_id = @owner_id AND recipient IS NULL;
