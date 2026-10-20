-- name: InsertSession :exec
INSERT INTO sessions (id, owner_id, email, display_name, token_hash, expires_at)
VALUES (@id, @owner_id, @email, @display_name, @token_hash, @expires_at);

-- name: GetSessionByTokenHash :one
-- owner-scope: global the secret token hash identifies the session
SELECT id, owner_id, email, display_name, expires_at
FROM sessions
WHERE token_hash = @token_hash;

-- name: ExtendSession :execrows
-- owner-scope: global session id comes from a session just resolved by its token hash
UPDATE sessions
SET expires_at = @expires_at, updated_at = now()
WHERE id = @id;

-- name: DeleteSessionByTokenHash :exec
-- owner-scope: global the secret token hash identifies the session
DELETE FROM sessions WHERE token_hash = @token_hash;

-- name: DeleteSessionsByOwner :execrows
DELETE FROM sessions WHERE owner_id = @owner_id;

-- name: DeleteExpiredSessions :execrows
-- owner-scope: global housekeeping over expired sessions
DELETE FROM sessions WHERE expires_at <= @now;
