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
-- means the first page. Each draft carries the subject and the start of the
-- body of its newest version, so the queue can be shown without a call each.
SELECT d.*, v.subject AS version_subject, COALESCE(left(v.body, 200), '')::text AS version_preview
FROM drafts d
LEFT JOIN draft_versions v ON v.draft_id = d.id AND v.version = d.current_version
WHERE d.owner_id = @owner_id
  AND d.state = @state::text
  AND (
    sqlc.narg(after_updated_at)::timestamptz IS NULL
    OR (d.updated_at, d.id) < (sqlc.narg(after_updated_at)::timestamptz, sqlc.narg(after_id)::uuid)
  )
ORDER BY d.updated_at DESC, d.id DESC
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
