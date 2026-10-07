-- +goose Up
-- matched_at is set the first time a posting's score reaches the owner's
-- minimum, in the transaction that emits discovery.match_found, so the event is
-- emitted once however often a posting is scored again.
ALTER TABLE scores ADD COLUMN matched_at timestamptz;

-- +goose Down
ALTER TABLE scores DROP COLUMN matched_at;
