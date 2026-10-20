-- name: InsertReservation :one
INSERT INTO reservations (id, owner_id, service, feature, model, est_micros, est_tokens, budget_period_ids, created_at, expires_at)
VALUES (@id, @owner_id, @service, @feature, @model, @est_micros, @est_tokens, @budget_period_ids, @created_at, @expires_at)
RETURNING *;

-- name: LockReservation :one
SELECT * FROM reservations WHERE id = @id AND owner_id = @owner_id FOR UPDATE;

-- name: LockReservationByID :one
-- owner-scope: internal expiry sweep; the id comes from ListExpiredReservationIDs
-- For the sweeper, which acts for no owner.
SELECT * FROM reservations WHERE id = @id FOR UPDATE;

-- name: SetReservationStatus :exec
-- owner-scope: child reservation already locked with the owner
UPDATE reservations SET status = @status WHERE id = @id;

-- name: ListExpiredReservationIDs :many
-- owner-scope: internal expiry sweep across all owners
SELECT id FROM reservations
WHERE status = 'open' AND expires_at <= @now
ORDER BY expires_at
LIMIT @batch_size;

-- name: SumOpenReservedTokens :one
-- Tokens held by the owner's calls that are still in flight.
SELECT COALESCE(sum(est_tokens), 0)::bigint AS tokens
FROM reservations
WHERE owner_id = @owner_id AND status = 'open' AND expires_at > @now;
