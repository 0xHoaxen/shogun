-- +goose Up
-- Default budgets for kagami's one AI feature, which maps the columns of an
-- imported contacts sheet. Each call sees only the header and a few rows, so
-- the limits are small. Like the rows in 00003 they are templates under the nil
-- owner, copied to a real user on first use.
-- TODO(owner): confirm these numbers.
INSERT INTO budgets (id, owner_id, scope_type, scope_value, period, limit_micros, mode)
VALUES ('00000000-0000-7000-8000-00000000000d', '00000000-0000-0000-0000-000000000000', 'service', 'kagami',                'monthly', 1000000, 'hard'),
       ('00000000-0000-7000-8000-00000000000e', '00000000-0000-0000-0000-000000000000', 'feature', 'kagami.import_reshape', 'daily',    200000, 'soft');

-- +goose Down
DELETE FROM budgets WHERE id IN ('00000000-0000-7000-8000-00000000000d', '00000000-0000-7000-8000-00000000000e');
