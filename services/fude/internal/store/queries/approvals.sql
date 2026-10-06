-- name: InsertApproval :one
INSERT INTO approvals (id, draft_id, version, token_jti, approved_by, approved_at, expires_at)
VALUES (@id, @draft_id, @version, @token_jti, @approved_by, @approved_at, @expires_at)
RETURNING *;
