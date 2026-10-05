-- +goose Up
-- Seed prices and default budgets.
-- TODO(owner): confirm the prices against the current price list and the
-- default budget numbers; both are editable in the app afterwards.

INSERT INTO prices (model, effective_from, input_micros_per_mtok, output_micros_per_mtok,
                    cache_read_micros_per_mtok, cache_write_micros_per_mtok)
VALUES ('claude-opus-5-5',   '2026-01-01', 4000000, 20000000, 200000, 5000000),
       ('claude-sonnet-5-5', '2026-01-01', 2000000, 10000000, 200000, 2500000),
       ('claude-haiku-4-5',  '2026-01-01', 1000000,  5000000, 100000, 1250000);

-- budgets.owner_id is NOT NULL but the owner is only known at login, so the
-- defaults are templates under the nil owner id. The app copies them to a
-- real owner on first use.
INSERT INTO budgets (id, owner_id, scope_type, scope_value, period, limit_micros, mode)
VALUES ('00000000-0000-7000-8000-000000000001', '00000000-0000-0000-0000-000000000000', 'global',  '',                 'monthly', 20000000, 'hard'),
       ('00000000-0000-7000-8000-000000000002', '00000000-0000-0000-0000-000000000000', 'service', 'fude',             'monthly', 15000000, 'hard'),
       ('00000000-0000-7000-8000-000000000003', '00000000-0000-0000-0000-000000000000', 'service', 'tsubame',          'monthly',  3000000, 'hard'),
       ('00000000-0000-7000-8000-000000000004', '00000000-0000-0000-0000-000000000000', 'service', 'katana',           'monthly',  3000000, 'hard'),
       ('00000000-0000-7000-8000-000000000005', '00000000-0000-0000-0000-000000000000', 'service', 'shinobi',          'monthly',  3000000, 'hard'),
       ('00000000-0000-7000-8000-000000000006', '00000000-0000-0000-0000-000000000000', 'feature', 'fude.cover_letter', 'daily',    1000000, 'soft'),
       ('00000000-0000-7000-8000-000000000007', '00000000-0000-0000-0000-000000000000', 'feature', 'fude.outreach',     'daily',    1000000, 'soft'),
       ('00000000-0000-7000-8000-000000000008', '00000000-0000-0000-0000-000000000000', 'feature', 'fude.post',         'daily',    1000000, 'soft'),
       ('00000000-0000-7000-8000-000000000009', '00000000-0000-0000-0000-000000000000', 'feature', 'tsubame.classify',  'daily',    1000000, 'soft'),
       ('00000000-0000-7000-8000-00000000000a', '00000000-0000-0000-0000-000000000000', 'feature', 'katana.suggest',    'daily',    1000000, 'soft'),
       ('00000000-0000-7000-8000-00000000000b', '00000000-0000-0000-0000-000000000000', 'feature', 'shinobi.score',     'daily',    1000000, 'soft');

-- +goose Down
DELETE FROM budgets WHERE owner_id = '00000000-0000-0000-0000-000000000000';
DELETE FROM prices WHERE effective_from = '2026-01-01';
