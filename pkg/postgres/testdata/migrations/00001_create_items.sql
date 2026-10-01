-- +goose Up
CREATE TABLE items (
    id   bigserial PRIMARY KEY,
    name text NOT NULL
);

-- +goose Down
DROP TABLE items;
