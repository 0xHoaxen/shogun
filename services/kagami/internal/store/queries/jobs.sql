-- name: InsertJob :one
INSERT INTO jobs (
    id, owner_id, company_id, title, url, source, status, applied_on,
    next_follow_up, location, salary_text, description, idempotency_key
) VALUES (
    @id, @owner_id, @company_id, @title, @url, @source, @status, @applied_on,
    @next_follow_up, @location, @salary_text, @description, @idempotency_key
)
RETURNING *;

-- name: GetJob :one
SELECT * FROM jobs WHERE id = @id AND owner_id = @owner_id;

-- name: GetJobByURL :one
SELECT * FROM jobs WHERE owner_id = @owner_id AND url = @url::text;

-- name: GetJobByIdempotencyKey :one
SELECT * FROM jobs WHERE owner_id = @owner_id AND idempotency_key = @idempotency_key::text;

-- name: ListJobs :many
-- Keyset pagination on (updated_at, id), newest first. A NULL after_* pair
-- means the first page.
SELECT * FROM jobs
WHERE owner_id = @owner_id
  AND archived_at IS NULL
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  AND (sqlc.narg(company_id)::uuid IS NULL OR company_id = sqlc.narg(company_id)::uuid)
  AND (sqlc.narg(due_before)::date IS NULL OR next_follow_up <= sqlc.narg(due_before)::date)
  AND (
    sqlc.narg(after_updated_at)::timestamptz IS NULL
    OR (updated_at, id) < (sqlc.narg(after_updated_at)::timestamptz, sqlc.narg(after_id)::uuid)
  )
ORDER BY updated_at DESC, id DESC
LIMIT @row_limit;

-- name: UpdateJob :one
-- Writes every editable column; the caller applies its field mask to a copy of
-- the current row first. Returns no row when the version is stale.
UPDATE jobs SET
    title = @title,
    url = @url,
    source = @source,
    applied_on = @applied_on,
    next_follow_up = @next_follow_up,
    location = @location,
    salary_text = @salary_text,
    description = @description,
    version = version + 1,
    updated_at = now()
WHERE id = @id AND owner_id = @owner_id AND version = @version
RETURNING *;

-- name: UpdateJobStatus :one
UPDATE jobs SET
    status = @status,
    applied_on = @applied_on,
    version = version + 1,
    updated_at = now()
WHERE id = @id AND owner_id = @owner_id AND version = @version
RETURNING *;

-- name: InsertJobEvent :one
INSERT INTO job_events (id, job_id, kind, from_status, to_status, source_event_id, payload, occurred_at)
VALUES (@id, @job_id, @kind, @from_status, @to_status, @source_event_id, @payload, @occurred_at)
RETURNING *;

-- name: ListJobEvents :many
SELECT * FROM job_events WHERE job_id = @job_id ORDER BY occurred_at DESC, id DESC;
