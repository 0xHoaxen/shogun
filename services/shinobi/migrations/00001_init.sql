-- +goose Up
-- Transactional outbox and idempotent inbox. Copy this snippet into a
-- service's first migration (see postgres.OutboxInboxSQL).

-- payload holds the proto-marshalled shogun.events.v1.Envelope, ready to be
-- delivered as-is. type, source and subject are denormalised for routing.
CREATE TABLE outbox (
    id           uuid PRIMARY KEY,
    type         text NOT NULL,
    source       text NOT NULL,
    subject      text NOT NULL DEFAULT '',
    payload      bytea NOT NULL,
    traceparent  text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now(),
    delivered_at timestamptz
);

CREATE INDEX outbox_undelivered_idx ON outbox (created_at) WHERE delivered_at IS NULL;

CREATE TABLE inbox (
    event_id    uuid PRIMARY KEY,
    received_at timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE inbox;
DROP TABLE outbox;
