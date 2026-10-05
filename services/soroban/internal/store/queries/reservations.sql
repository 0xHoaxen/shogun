-- name: InsertReservation :one
INSERT INTO reservations (id, owner_id, service, feature, model, est_micros, budget_period_ids, created_at, expires_at)
VALUES (@id, @owner_id, @service, @feature, @model, @est_micros, @budget_period_ids, @created_at, @expires_at)
RETURNING *;
