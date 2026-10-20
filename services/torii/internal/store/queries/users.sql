-- name: UpsertUser :one
-- owner-scope: global users are the tenants; the id is derived from the Google subject at login
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
RETURNING *, (xmax = 0) AS inserted;

-- name: GetUser :one
-- owner-scope: global users are the tenants; their id is the owner id; admin calls are role-checked in torii
SELECT * FROM users WHERE id = @id AND archived_at IS NULL;

-- name: ListUsers :many
-- owner-scope: global users are the tenants; only admin RPCs list them, role-checked in torii
SELECT * FROM users
WHERE archived_at IS NULL
  AND (created_at, id) > (@after_created_at::timestamptz, @after_id::uuid)
ORDER BY created_at, id
LIMIT @page_size;

-- name: SetUserRole :one
-- owner-scope: global users are the tenants; only the admin bootstrap changes it
UPDATE users SET role = @role, version = version + 1, updated_at = now()
WHERE id = @id AND archived_at IS NULL
RETURNING *;

-- name: SetUserStatus :one
-- owner-scope: global users are the tenants; only admin RPCs change it, role-checked in torii
UPDATE users SET status = @status, version = version + 1, updated_at = now()
WHERE id = @id AND archived_at IS NULL
RETURNING *;

-- name: SetUserGmailAllowed :one
-- owner-scope: global users are the tenants; only admin RPCs change it, role-checked in torii
UPDATE users SET gmail_allowed = @gmail_allowed, version = version + 1, updated_at = now()
WHERE id = @id AND archived_at IS NULL
RETURNING *;

-- name: SetUserTimezone :one
-- owner-scope: global users are the tenants; a user changes only their own, by session id
UPDATE users SET timezone = @timezone, version = version + 1, updated_at = now()
WHERE id = @id AND archived_at IS NULL
RETURNING *;
