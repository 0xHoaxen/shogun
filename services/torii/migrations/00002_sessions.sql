-- +goose Up
-- Browser sessions. Only the SHA-256 of the session token is stored, so a
-- database leak does not yield usable cookies.
CREATE TABLE sessions (
    id           uuid PRIMARY KEY,
    owner_id     uuid NOT NULL,
    email        text NOT NULL,
    display_name text NOT NULL DEFAULT '',
    token_hash   bytea NOT NULL UNIQUE,
    expires_at   timestamptz NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX sessions_owner_idx ON sessions (owner_id);
CREATE INDEX sessions_expires_idx ON sessions (expires_at);

-- +goose Down
DROP TABLE sessions;
