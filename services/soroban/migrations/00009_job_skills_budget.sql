-- +goose Up
-- Default budget for katana's job skills: one small call per saved job, reading
-- up to about 3k tokens of posting. Like the rows in 00003 it is a template
-- under the nil owner, copied to a real user on first use.
-- TODO(owner): confirm this number.
INSERT INTO budgets (id, owner_id, scope_type, scope_value, period, limit_micros, mode)
VALUES ('00000000-0000-7000-8000-000000000010', '00000000-0000-0000-0000-000000000000', 'feature', 'katana.job_skills', 'daily', 200000, 'soft');

-- +goose Down
DELETE FROM budgets WHERE id = '00000000-0000-7000-8000-000000000010';
