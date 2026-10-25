-- +goose Up
-- Default budget for katana's resume parsing: one call per uploaded resume,
-- up to about 8k tokens in and 3k out. Like the rows in 00003 it is a template
-- under the nil owner, copied to a real user on first use. The katana service
-- budget in 00003 already covers the month.
-- TODO(owner): confirm this number.
INSERT INTO budgets (id, owner_id, scope_type, scope_value, period, limit_micros, mode)
VALUES ('00000000-0000-7000-8000-00000000000f', '00000000-0000-0000-0000-000000000000', 'feature', 'katana.resume_parse', 'daily', 300000, 'soft');

-- +goose Down
DELETE FROM budgets WHERE id = '00000000-0000-7000-8000-00000000000f';
