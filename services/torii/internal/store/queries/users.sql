-- name: UpsertUser :one
-- Creates the user, or refreshes email and display name on a later login.
-- Role, status, gmail_allowed and timezone are only changed by their setters.
INSERT INTO users (id, google_sub, email, display_name)
VALUES (@id, @google_sub, @email, @display_name)
ON CONFLICT (google_sub) DO UPDATE
SET email = EXCLUDED.email,
    display_name = EXCLUDED.display_name,
    version = CASE WHEN users.email <> EXCLUDED.email OR users.display_name <> EXCLUDED.display_name
                   THEN users.version + 1 ELSE users.version END,
    updated_at = CASE WHEN users.email <> EXCLUDED.email OR users.display_name <> EXCLUDED.display_name
                      THEN now() ELSE users.updated_at END
RETURNING *;

-- name: GetUser :one
SELECT * FROM users WHERE id = @id AND archived_at IS NULL;

-- name: ListUsers :many
SELECT * FROM users
WHERE archived_at IS NULL
  AND (created_at, id) > (@after_created_at::timestamptz, @after_id::uuid)
ORDER BY created_at, id
LIMIT @page_size;

-- name: SetUserRole :one
UPDATE users SET role = @role, version = version + 1, updated_at = now()
WHERE id = @id AND archived_at IS NULL
RETURNING *;

-- name: SetUserStatus :one
UPDATE users SET status = @status, version = version + 1, updated_at = now()
WHERE id = @id AND archived_at IS NULL
RETURNING *;

-- name: SetUserGmailAllowed :one
UPDATE users SET gmail_allowed = @gmail_allowed, version = version + 1, updated_at = now()
WHERE id = @id AND archived_at IS NULL
RETURNING *;

-- name: SetUserTimezone :one
UPDATE users SET timezone = @timezone, version = version + 1, updated_at = now()
WHERE id = @id AND archived_at IS NULL
RETURNING *;
