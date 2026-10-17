-- name: GetGitHubAccount :one
SELECT * FROM github_accounts WHERE owner_id = @owner_id;

-- name: UpsertGitHubAccount :one
-- Connecting again replaces the login and the token.
INSERT INTO github_accounts (owner_id, login, token_ciphertext, key_id)
VALUES (@owner_id, @login, @token_ciphertext, @key_id)
ON CONFLICT (owner_id) DO UPDATE
SET login = EXCLUDED.login, token_ciphertext = EXCLUDED.token_ciphertext,
    key_id = EXCLUDED.key_id, connected_at = now()
RETURNING *;

-- name: DeleteGitHubAccount :execrows
DELETE FROM github_accounts WHERE owner_id = @owner_id;

-- name: ListGitHubAccountOwners :many
-- owner-scope: internal the daily sync lists every connected owner, then syncs each one separately
SELECT owner_id FROM github_accounts ORDER BY owner_id;
