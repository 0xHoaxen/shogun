-- +goose Up
-- One GitHub connection per user. The access token is sealed with envelope
-- encryption (AES-256-GCM, a data key per value wrapped by a master key named by
-- key_id) and bound to the owner, so a copied row will not open under another
-- user. A user with no row has no GitHub sync.
CREATE TABLE github_accounts (
    owner_id         uuid PRIMARY KEY,
    login            text NOT NULL,
    token_ciphertext bytea NOT NULL,
    key_id           text NOT NULL,
    connected_at     timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE github_accounts;
