-- name: GetTemplate :one
-- contact_status is NULL for templates that are not tied to a contact status.
SELECT * FROM templates
WHERE owner_id = @owner_id AND kind = @kind AND channel = @channel
  AND contact_status IS NOT DISTINCT FROM sqlc.narg(contact_status)::text;
