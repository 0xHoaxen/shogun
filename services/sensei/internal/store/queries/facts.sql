-- name: InsertFact :execrows
-- One fact per event id: a redelivered event stores nothing.
INSERT INTO facts (event_id, owner_id, type, dimension, occurred_at)
VALUES (@event_id, @owner_id, @type, @dimension, @occurred_at)
ON CONFLICT (event_id) DO NOTHING;

-- name: CountFacts :one
SELECT count(*) FROM facts WHERE owner_id = @owner_id AND type = @type;
