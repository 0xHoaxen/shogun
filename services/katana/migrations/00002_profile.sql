-- +goose Up
CREATE TABLE github_snapshots (
    id            uuid PRIMARY KEY,
    owner_id      uuid NOT NULL,
    taken_at      timestamptz NOT NULL DEFAULT now(),
    repos         jsonb NOT NULL,
    contributions jsonb NOT NULL,
    etag          text
);

CREATE INDEX github_snapshots_latest ON github_snapshots (owner_id, taken_at DESC, id DESC);

CREATE TABLE suggestions (
    id         uuid PRIMARY KEY,
    owner_id   uuid NOT NULL,
    target     text NOT NULL CHECK (target IN ('resume', 'linkedin')),
    section    text NOT NULL,
    before     text,
    after      text NOT NULL,
    reason     text NOT NULL,
    evidence   jsonb NOT NULL DEFAULT '[]',
    state      text NOT NULL DEFAULT 'open' CHECK (state IN ('open', 'accepted', 'dismissed')),
    created_at timestamptz NOT NULL DEFAULT now(),
    decided_at timestamptz
);

CREATE INDEX suggestions_open ON suggestions (owner_id, created_at DESC) WHERE state = 'open';
CREATE INDEX suggestions_owner_created ON suggestions (owner_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE suggestions;
DROP TABLE github_snapshots;
