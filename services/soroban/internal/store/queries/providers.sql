-- name: GetProvider :one
SELECT * FROM providers WHERE owner_id = @owner_id;

-- name: InsertProvider :one
INSERT INTO providers (owner_id, base_url, model, api_key_ciphertext, key_id, key_last4,
                       input_micros_per_mtok, output_micros_per_mtok)
VALUES (@owner_id, @base_url, @model, @api_key_ciphertext, @key_id, @key_last4,
        @input_micros_per_mtok, @output_micros_per_mtok)
RETURNING *;

-- name: UpdateProvider :one
-- The row changes only if version is the one the caller read.
UPDATE providers
SET base_url = @base_url, model = @model, api_key_ciphertext = @api_key_ciphertext,
    key_id = @key_id, key_last4 = @key_last4,
    input_micros_per_mtok = @input_micros_per_mtok, output_micros_per_mtok = @output_micros_per_mtok,
    version = version + 1, updated_at = now()
WHERE owner_id = @owner_id AND version = @version
RETURNING *;

-- name: DeleteProvider :execrows
DELETE FROM providers WHERE owner_id = @owner_id;
