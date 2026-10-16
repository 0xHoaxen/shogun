-- +goose Up
-- A user may cap their AI tokens per month, which is the only limit that works
-- for a model nobody has a price for. NULL means unlimited.
ALTER TABLE providers ADD COLUMN monthly_token_cap bigint CHECK (monthly_token_cap > 0);

-- Tokens a reservation holds, so a cap counts calls still in flight.
ALTER TABLE reservations ADD COLUMN est_tokens bigint NOT NULL DEFAULT 0;

-- +goose Down
ALTER TABLE reservations DROP COLUMN est_tokens;
ALTER TABLE providers DROP COLUMN monthly_token_cap;
