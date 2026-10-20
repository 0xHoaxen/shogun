-- +goose Up
-- Who the users are, as far as this service needs to know: enough to run its
-- schedules in each user's own time zone. Filled from torii's user.* events;
-- version only goes up, so a late or repeated event changes nothing. Copy this
-- snippet into a service's migration (see owners.SQL).
CREATE TABLE owners (
    id         uuid PRIMARY KEY,
    timezone   text NOT NULL,
    status     text NOT NULL CHECK (status IN ('active', 'disabled', 'deleted')),
    version    int NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX owners_active_idx ON owners (id) WHERE status = 'active';

-- +goose Down
DROP TABLE owners;
