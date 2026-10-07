-- +goose Up
CREATE TABLE facts (
    event_id    uuid PRIMARY KEY,
    owner_id    uuid NOT NULL,
    type        text NOT NULL,
    dimension   jsonb NOT NULL DEFAULT '{}',
    occurred_at timestamptz NOT NULL
);

CREATE INDEX facts_type_time ON facts (owner_id, type, occurred_at);

CREATE TABLE daily_rollups (
    owner_id  uuid NOT NULL,
    day       date NOT NULL,
    metric    text NOT NULL,
    dimension text NOT NULL DEFAULT '',
    value     bigint NOT NULL,
    PRIMARY KEY (owner_id, day, metric, dimension)
);

-- +goose Down
DROP TABLE daily_rollups;
DROP TABLE facts;
