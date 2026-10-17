-- +goose Up
-- The default budget of the feature behind "test connection" in Settings: a tiny
-- call, so a small soft daily limit that only warns. Like the rows in 00003 it is
-- a template under the nil owner, copied to a real user on first use.
INSERT INTO budgets (id, owner_id, scope_type, scope_value, period, limit_micros, mode)
VALUES ('00000000-0000-7000-8000-00000000000c', '00000000-0000-0000-0000-000000000000', 'feature', 'fude.test_connection', 'daily', 100000, 'soft');

-- +goose Down
DELETE FROM budgets WHERE id = '00000000-0000-7000-8000-00000000000c';
