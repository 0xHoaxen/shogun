-- +goose Up
-- One AI provider per user: an Anthropic-compatible base URL, a model and an
-- API key. The key is sealed with envelope encryption (AES-256-GCM, a data key
-- per value wrapped by a master key named by key_id) and bound to the owner, so
-- a copied row will not open under another user. A user with no row has no AI
-- features. Prices are only for models soroban has no price for.
CREATE TABLE providers (
    owner_id               uuid PRIMARY KEY,
    base_url               text NOT NULL,
    model                  text NOT NULL,
    api_key_ciphertext     bytea NOT NULL,
    key_id                 text NOT NULL,
    key_last4              text NOT NULL,
    input_micros_per_mtok  bigint CHECK (input_micros_per_mtok >= 0),
    output_micros_per_mtok bigint CHECK (output_micros_per_mtok >= 0),
    version                int NOT NULL DEFAULT 1,
    created_at             timestamptz NOT NULL DEFAULT now(),
    updated_at             timestamptz NOT NULL DEFAULT now(),
    -- A price is either both directions or none.
    CHECK ((input_micros_per_mtok IS NULL) = (output_micros_per_mtok IS NULL))
);

-- +goose Down
DROP TABLE providers;
