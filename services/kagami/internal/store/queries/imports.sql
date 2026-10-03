-- name: InsertImport :one
INSERT INTO imports (id, owner_id, filename, rows_total, rows_created, rows_updated, rows_failed, errors)
VALUES (@id, @owner_id, @filename, @rows_total, @rows_created, @rows_updated, @rows_failed, @errors)
RETURNING *;
