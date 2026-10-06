-- name: InsertNotification :one
-- Returns no row when a notification for source_event_id already exists, so a
-- redelivered event adds nothing.
INSERT INTO notifications (id, owner_id, type, title, body, link, source_event_id, created_at)
VALUES (@id, @owner_id, @type, @title, @body, @link, @source_event_id, @created_at)
ON CONFLICT (source_event_id) DO NOTHING
RETURNING *;

-- name: ListNotifications :many
-- Keyset pagination on (created_at, id), newest first. A NULL after_* pair
-- means the first page.
SELECT * FROM notifications
WHERE owner_id = @owner_id
  AND (NOT @unread_only::boolean OR read_at IS NULL)
  AND (
    sqlc.narg(after_created_at)::timestamptz IS NULL
    OR (created_at, id) < (sqlc.narg(after_created_at)::timestamptz, sqlc.narg(after_id)::uuid)
  )
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;

-- name: ListNotificationsAfter :many
-- Replays what a stream missed. Ids are UUIDv7, so id order is creation order.
SELECT * FROM notifications
WHERE owner_id = @owner_id AND id > @after_id
ORDER BY id ASC
LIMIT @row_limit;

-- name: CountUnread :one
SELECT count(*)::int FROM notifications
WHERE owner_id = @owner_id AND read_at IS NULL;

-- name: MarkNotificationsRead :execrows
UPDATE notifications SET read_at = @read_at
WHERE owner_id = @owner_id AND id = ANY(@ids::uuid[]) AND read_at IS NULL;

-- name: MarkAllNotificationsRead :execrows
UPDATE notifications SET read_at = @read_at
WHERE owner_id = @owner_id AND read_at IS NULL;

-- name: GetNotification :one
SELECT * FROM notifications WHERE owner_id = @owner_id AND id = @id;
