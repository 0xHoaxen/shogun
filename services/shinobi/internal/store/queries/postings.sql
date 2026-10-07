-- name: UpsertPosting :one
-- A posting seen again is refreshed in place; inserted says whether it is new.
INSERT INTO postings (id, owner_id, source_id, external_id, title, company, url, location, posted_at, raw, created_at)
VALUES (@id, @owner_id, @source_id, @external_id, @title, @company, @url, @location, @posted_at, @raw, @created_at)
ON CONFLICT (source_id, external_id) DO UPDATE SET
    title = EXCLUDED.title, company = EXCLUDED.company, url = EXCLUDED.url,
    location = EXCLUDED.location, posted_at = EXCLUDED.posted_at, raw = EXCLUDED.raw
RETURNING id, owner_id, source_id, external_id, title, company, url, location, posted_at, raw, saved_job_id, created_at,
          (xmax = 0)::boolean AS inserted;

-- name: GetPosting :one
SELECT * FROM postings WHERE owner_id = @owner_id AND id = @id;

-- name: GetPostingByID :one
-- For a job that works for an owner it already knows.
SELECT * FROM postings WHERE id = @id;

-- name: ListPostings :many
-- Best score first, unscored last. Keyset pagination on (score, id), where an
-- unscored posting counts as -1; a NULL after_id means the first page.
SELECT p.id, p.owner_id, p.source_id, p.external_id, p.title, p.company, p.url, p.location, p.posted_at,
       p.saved_job_id, p.created_at, s.score, s.reasons, s.scored_by
FROM postings p
LEFT JOIN scores s ON s.posting_id = p.id
WHERE p.owner_id = @owner_id
  AND (sqlc.narg(min_score)::real IS NULL OR s.score >= sqlc.narg(min_score)::real)
  AND (sqlc.narg(source_id)::uuid IS NULL OR p.source_id = sqlc.narg(source_id)::uuid)
  AND (
    sqlc.narg(after_id)::uuid IS NULL
    OR (COALESCE(s.score, -1)::real, p.id) < (sqlc.narg(after_score)::real, sqlc.narg(after_id)::uuid)
  )
ORDER BY COALESCE(s.score, -1)::real DESC, p.id DESC
LIMIT @row_limit;

-- name: ListUnscoredPostingIDs :many
SELECT p.id FROM postings p
LEFT JOIN scores s ON s.posting_id = p.id
WHERE p.source_id = @source_id AND s.posting_id IS NULL
ORDER BY p.id;

-- name: SetSavedJob :one
-- Records the kagami job once; a posting already saved keeps its first job.
UPDATE postings SET saved_job_id = @saved_job_id
WHERE owner_id = @owner_id AND id = @id AND saved_job_id IS NULL
RETURNING *;

-- name: UpsertScore :exec
INSERT INTO scores (posting_id, score, reasons, scored_by, created_at)
VALUES (@posting_id, @score, @reasons, @scored_by, @created_at)
ON CONFLICT (posting_id) DO UPDATE SET
    score = EXCLUDED.score, reasons = EXCLUDED.reasons, scored_by = EXCLUDED.scored_by, created_at = EXCLUDED.created_at;

-- name: GetScore :one
SELECT * FROM scores WHERE posting_id = @posting_id;

-- name: MarkMatched :one
-- Sets matched_at once. It returns a row only the first time, so the caller
-- emits the match event exactly then.
UPDATE scores SET matched_at = @matched_at
WHERE posting_id = @posting_id AND matched_at IS NULL
RETURNING posting_id;
