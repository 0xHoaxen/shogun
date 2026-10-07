-- name: InsertSnapshot :one
INSERT INTO github_snapshots (id, owner_id, taken_at, repos, contributions, etag)
VALUES (@id, @owner_id, @taken_at, @repos, @contributions, @etag)
RETURNING *;

-- name: ListLatestSnapshots :many
-- The newest snapshots of the owner, newest first, for the ETag and the diff.
SELECT * FROM github_snapshots
WHERE owner_id = @owner_id
ORDER BY taken_at DESC, id DESC
LIMIT @row_limit;

-- name: ListSnapshotOwners :many
-- Owners who have synced at least once; the daily sync covers them.
SELECT DISTINCT owner_id FROM github_snapshots ORDER BY owner_id;
