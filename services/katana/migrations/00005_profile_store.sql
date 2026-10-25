-- +goose Up
-- The profile an owner keeps after checking a parsed resume: one profile per
-- owner, its roles as an ordered list, and skills with a level. No foreign key
-- joins them, so a skill can exist before a profile does.
CREATE TABLE profiles (
    owner_id         uuid PRIMARY KEY,
    name             text NOT NULL DEFAULT '',
    headline         text NOT NULL DEFAULT '',
    location         text NOT NULL DEFAULT '',
    email            text NOT NULL DEFAULT '',
    years_experience double precision CHECK (years_experience BETWEEN 0 AND 60),
    version          integer NOT NULL DEFAULT 1,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now()
);

-- Replaced as a whole list on every save. Months are YYYY-MM.
CREATE TABLE profile_roles (
    owner_id    uuid NOT NULL,
    position    integer NOT NULL,
    title       text NOT NULL DEFAULT '',
    company     text NOT NULL DEFAULT '',
    start_month text CHECK (start_month ~ '^\d{4}-(0[1-9]|1[0-2])$'),
    end_month   text CHECK (end_month ~ '^\d{4}-(0[1-9]|1[0-2])$'),
    is_current  boolean NOT NULL DEFAULT false,
    PRIMARY KEY (owner_id, position)
);

-- name_key is the lower-cased name, so "Go" and "go" are one skill. A level
-- only goes up (see UpsertSkill).
CREATE TABLE skills (
    owner_id   uuid NOT NULL,
    name_key   text NOT NULL,
    name       text NOT NULL,
    level      smallint NOT NULL CHECK (level BETWEEN 0 AND 10),
    source     text NOT NULL CHECK (source IN ('resume', 'manual', 'job')),
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, name_key)
);

-- +goose Down
DROP TABLE skills;
DROP TABLE profile_roles;
DROP TABLE profiles;
