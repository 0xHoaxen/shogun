-- name: InsertSession :exec
INSERT INTO sessions (id, owner_id, email, display_name, token_hash, expires_at)
VALUES (@id, @owner_id, @email, @display_name, @token_hash, @expires_at);

-- name: GetSessionByTokenHash :one
SELECT id, owner_id, email, display_name, expires_at
FROM sessions
WHERE token_hash = @token_hash;

-- name: ExtendSession :execrows
UPDATE sessions
SET expires_at = @expires_at, updated_at = now()
WHERE id = @id;

-- name: DeleteSessionByTokenHash :exec
DELETE FROM sessions WHERE token_hash = @token_hash;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions WHERE expires_at <= @now;
