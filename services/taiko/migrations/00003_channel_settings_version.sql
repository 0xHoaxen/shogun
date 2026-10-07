-- +goose Up
ALTER TABLE channel_settings
    ADD COLUMN version    bigint      NOT NULL DEFAULT 1,
    ADD COLUMN updated_at timestamptz NOT NULL DEFAULT now();

-- +goose Down
ALTER TABLE channel_settings
    DROP COLUMN updated_at,
    DROP COLUMN version;
