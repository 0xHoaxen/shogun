-- name: InsertReservation :one
INSERT INTO reservations (id, owner_id, service, feature, model, est_micros, budget_period_ids, created_at, expires_at)
VALUES (@id, @owner_id, @service, @feature, @model, @est_micros, @budget_period_ids, @created_at, @expires_at)
RETURNING *;

-- name: LockReservation :one
SELECT * FROM reservations WHERE id = @id AND owner_id = @owner_id FOR UPDATE;

-- name: LockReservationByID :one
-- For the sweeper, which acts for no owner.
SELECT * FROM reservations WHERE id = @id FOR UPDATE;

-- name: SetReservationStatus :exec
UPDATE reservations SET status = @status WHERE id = @id;

-- name: ListExpiredReservationIDs :many
SELECT id FROM reservations
WHERE status = 'open' AND expires_at <= @now
ORDER BY expires_at
LIMIT @batch_size;
