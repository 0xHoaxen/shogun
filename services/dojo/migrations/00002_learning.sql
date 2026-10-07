-- +goose Up
CREATE TABLE items (
    id           uuid PRIMARY KEY,
    owner_id     uuid NOT NULL,
    title        text NOT NULL,
    kind         text NOT NULL CHECK (kind IN ('course', 'book', 'project', 'skill')),
    url          text,
    status       text NOT NULL DEFAULT 'planned' CHECK (status IN ('planned', 'in_progress', 'done')),
    started_on   date,
    completed_on date,
    insight      text,
    version      int NOT NULL DEFAULT 1,
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    archived_at  timestamptz
);

CREATE INDEX items_owner_created ON items (owner_id, created_at DESC, id DESC) WHERE archived_at IS NULL;

CREATE TABLE activities (
    id          uuid PRIMARY KEY,
    owner_id    uuid NOT NULL,
    item_id     uuid REFERENCES items (id),
    summary     text NOT NULL,
    minutes     int,
    occurred_on date NOT NULL DEFAULT current_date,
    tags        text[] NOT NULL DEFAULT '{}',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX activities_item ON activities (item_id, occurred_on DESC);
CREATE INDEX activities_owner_created ON activities (owner_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE activities;
DROP TABLE items;
