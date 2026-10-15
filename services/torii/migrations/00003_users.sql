-- +goose Up
-- One row per Google account. id is the owner id derived from the Google
-- subject, so it is the owner_id every other service stores.
CREATE TABLE users (
    id            uuid PRIMARY KEY,
    google_sub    text NOT NULL UNIQUE,
    email         text NOT NULL,
    display_name  text NOT NULL DEFAULT '',
    role          text NOT NULL DEFAULT 'member' CHECK (role IN ('admin', 'member')),
    status        text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    -- gmail_allowed is the admin approval for Connect Gmail, needed while the
    -- Google app is unverified.
    gmail_allowed boolean NOT NULL DEFAULT false,
    timezone      text NOT NULL DEFAULT 'Asia/Kolkata',
    version       int NOT NULL DEFAULT 1,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    archived_at   timestamptz
);

CREATE INDEX users_created_idx ON users (created_at, id);
CREATE INDEX users_email_idx ON users (lower(email));

-- +goose Down
DROP TABLE users;
