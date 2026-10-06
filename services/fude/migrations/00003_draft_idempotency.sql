-- +goose Up
-- A retried GenerateDraft with the same key returns the original draft.

ALTER TABLE drafts ADD COLUMN idempotency_key text;
CREATE UNIQUE INDEX drafts_idempotency ON drafts (owner_id, idempotency_key) WHERE idempotency_key IS NOT NULL;

-- +goose Down
DROP INDEX drafts_idempotency;
ALTER TABLE drafts DROP COLUMN idempotency_key;
