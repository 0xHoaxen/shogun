-- name: InsertContact :one
INSERT INTO contacts (
    id, owner_id, full_name, company_id, role, email, linkedin_url, x_handle,
    phone, relationship, how_we_met, status, preferred_channel, last_contacted,
    next_follow_up, target_role, job_id, tags, notes, idempotency_key
) VALUES (
    @id, @owner_id, @full_name, @company_id, @role, @email, @linkedin_url, @x_handle,
    @phone, @relationship, @how_we_met, @status, @preferred_channel, @last_contacted,
    @next_follow_up, @target_role, @job_id, @tags, @notes, @idempotency_key
)
RETURNING *;

-- name: GetContact :one
SELECT * FROM contacts WHERE id = @id AND owner_id = @owner_id;

-- name: GetContactByIdempotencyKey :one
SELECT * FROM contacts WHERE owner_id = @owner_id AND idempotency_key = @idempotency_key::text;

-- name: FindContactByEmail :one
-- email is citext, so the match ignores case.
SELECT * FROM contacts WHERE owner_id = @owner_id AND email = @email::citext;

-- name: FindContactByLinkedin :one
SELECT * FROM contacts WHERE owner_id = @owner_id AND linkedin_url = @linkedin_url::text;

-- name: ListContacts :many
-- Keyset pagination on (updated_at, id), newest first.
SELECT c.* FROM contacts c
LEFT JOIN companies co ON co.id = c.company_id
WHERE c.owner_id = @owner_id
  AND c.archived_at IS NULL
  AND (sqlc.narg(status)::text IS NULL OR c.status = sqlc.narg(status)::text)
  AND (sqlc.narg(tag)::text IS NULL OR sqlc.narg(tag)::text = ANY (c.tags))
  AND (sqlc.narg(company_id)::uuid IS NULL OR c.company_id = sqlc.narg(company_id)::uuid)
  AND (
    sqlc.narg(query)::text IS NULL
    OR c.full_name ILIKE '%' || sqlc.narg(query)::text || '%'
    OR c.email::text ILIKE '%' || sqlc.narg(query)::text || '%'
    OR co.name ILIKE '%' || sqlc.narg(query)::text || '%'
  )
  AND (
    sqlc.narg(after_updated_at)::timestamptz IS NULL
    OR (c.updated_at, c.id) < (sqlc.narg(after_updated_at)::timestamptz, sqlc.narg(after_id)::uuid)
  )
ORDER BY c.updated_at DESC, c.id DESC
LIMIT @row_limit;

-- name: UpdateContact :one
-- Writes every editable column; the caller applies its field mask to a copy of
-- the current row first. Returns no row when the version is stale.
UPDATE contacts SET
    full_name = @full_name,
    company_id = @company_id,
    role = @role,
    email = @email,
    linkedin_url = @linkedin_url,
    x_handle = @x_handle,
    phone = @phone,
    relationship = @relationship,
    how_we_met = @how_we_met,
    preferred_channel = @preferred_channel,
    last_contacted = @last_contacted,
    next_follow_up = @next_follow_up,
    target_role = @target_role,
    job_id = @job_id,
    tags = @tags,
    notes = @notes,
    version = version + 1,
    updated_at = now()
WHERE id = @id AND owner_id = @owner_id AND version = @version
RETURNING *;

-- name: UpdateContactStatus :one
UPDATE contacts SET
    status = @status,
    last_contacted = @last_contacted,
    version = version + 1,
    updated_at = now()
WHERE id = @id AND owner_id = @owner_id AND version = @version
RETURNING *;

-- name: ListContactsDueOn :many
-- Every owner's contacts with a follow-up on exactly this date.
SELECT * FROM contacts
WHERE next_follow_up = @due_on::date AND archived_at IS NULL
ORDER BY owner_id, id;

-- name: ListDueContacts :many
SELECT * FROM contacts
WHERE owner_id = @owner_id AND next_follow_up <= @on_or_before::date AND archived_at IS NULL
ORDER BY next_follow_up, id
LIMIT @row_limit;

-- name: InsertContactEvent :one
INSERT INTO contact_events (id, contact_id, kind, channel, from_status, to_status, source_event_id, payload, occurred_at)
VALUES (@id, @contact_id, @kind, @channel, @from_status, @to_status, @source_event_id, @payload, @occurred_at)
RETURNING *;

-- name: ListContactEvents :many
SELECT * FROM contact_events WHERE contact_id = @contact_id ORDER BY occurred_at DESC, id DESC;

-- name: LatestContactStatusAnchor :one
-- See LatestJobStatusAnchor.
SELECT COALESCE((payload->>'event_at')::timestamptz, occurred_at)::timestamptz AS anchor
FROM contact_events
WHERE contact_id = @contact_id AND kind = 'status_changed'
ORDER BY COALESCE((payload->>'event_at')::timestamptz, occurred_at) DESC
LIMIT 1;
