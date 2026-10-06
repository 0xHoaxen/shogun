-- name: UpsertAccount :one
-- Connecting an address again replaces its token and clears a reauth or
-- disabled status; its sync cursor is kept.
INSERT INTO accounts (id, owner_id, provider, address, token_ciphertext, token_key_id)
VALUES (@id, @owner_id, @provider, @address, @token_ciphertext, @token_key_id)
ON CONFLICT (owner_id, provider, address) DO UPDATE SET
    token_ciphertext = EXCLUDED.token_ciphertext,
    token_key_id = EXCLUDED.token_key_id,
    status = 'active',
    updated_at = now()
RETURNING *;

-- name: GetAccount :one
SELECT * FROM accounts WHERE id = @id AND owner_id = @owner_id;

-- name: ListAccounts :many
SELECT * FROM accounts WHERE owner_id = @owner_id ORDER BY created_at, id;

-- name: SetAccountStatus :execrows
UPDATE accounts SET status = @status, updated_at = now()
WHERE id = @id AND owner_id = @owner_id;

-- name: ListActiveAccounts :many
-- Every owner's accounts that can be synced.
SELECT * FROM accounts WHERE status = 'active' ORDER BY id;

-- name: SetAccountCursor :execrows
UPDATE accounts SET history_id = @history_id, last_synced_at = @synced_at, updated_at = now()
WHERE id = @id;
