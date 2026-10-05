-- +goose Up
-- Core soroban tables, as in docs/shogun-ddl.md. Money is bigint micro-dollars.

CREATE TABLE prices (
    model                       text   NOT NULL,
    effective_from              date   NOT NULL,
    input_micros_per_mtok       bigint NOT NULL,
    output_micros_per_mtok      bigint NOT NULL,
    cache_read_micros_per_mtok  bigint NOT NULL DEFAULT 0,
    cache_write_micros_per_mtok bigint NOT NULL DEFAULT 0,
    PRIMARY KEY (model, effective_from)
);

CREATE TABLE budgets (
    id           uuid PRIMARY KEY,
    owner_id     uuid NOT NULL,
    scope_type   text NOT NULL CHECK (scope_type IN ('global', 'service', 'feature')),
    scope_value  text NOT NULL DEFAULT '', -- '' | 'fude' | 'fude.cover_letter'
    period       text NOT NULL CHECK (period IN ('daily', 'monthly')),
    limit_micros bigint NOT NULL CHECK (limit_micros >= 0),
    mode         text NOT NULL CHECK (mode IN ('hard', 'soft')),
    thresholds   int[] NOT NULL DEFAULT '{50,80,100}',
    enabled      boolean NOT NULL DEFAULT true,
    version      int NOT NULL DEFAULT 1,
    updated_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, scope_type, scope_value, period)
);

CREATE TABLE budget_periods (
    id                  uuid PRIMARY KEY,
    budget_id           uuid NOT NULL REFERENCES budgets (id),
    period_start        timestamptz NOT NULL,
    period_end          timestamptz NOT NULL,
    spent_micros        bigint NOT NULL DEFAULT 0,
    reserved_micros     bigint NOT NULL DEFAULT 0,
    notified_thresholds int[] NOT NULL DEFAULT '{}',
    UNIQUE (budget_id, period_start)
);

CREATE TABLE reservations (
    id                uuid PRIMARY KEY,
    owner_id          uuid NOT NULL,
    service           text NOT NULL,
    feature           text NOT NULL,
    model             text NOT NULL,
    est_micros        bigint NOT NULL,
    budget_period_ids uuid[] NOT NULL,
    status            text NOT NULL DEFAULT 'open'
                      CHECK (status IN ('open', 'committed', 'released', 'expired')),
    created_at        timestamptz NOT NULL DEFAULT now(),
    expires_at        timestamptz NOT NULL
);
CREATE INDEX reservations_open ON reservations (expires_at) WHERE status = 'open';

CREATE TABLE ledger (
    id                 uuid PRIMARY KEY,
    owner_id           uuid NOT NULL,
    reservation_id     uuid NOT NULL UNIQUE REFERENCES reservations (id),
    service            text NOT NULL,
    feature            text NOT NULL,
    model              text NOT NULL,
    input_tokens       int  NOT NULL,
    output_tokens      int  NOT NULL,
    cache_read_tokens  int  NOT NULL DEFAULT 0,
    cache_write_tokens int  NOT NULL DEFAULT 0,
    cost_micros        bigint NOT NULL,
    request_id         text,
    occurred_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ledger_time ON ledger (owner_id, occurred_at);
CREATE INDEX ledger_feature ON ledger (owner_id, feature, occurred_at);

-- +goose Down
DROP TABLE ledger;
DROP TABLE reservations;
DROP TABLE budget_periods;
DROP TABLE budgets;
DROP TABLE prices;
