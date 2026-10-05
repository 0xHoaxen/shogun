-- name: InsertLedgerEntry :one
INSERT INTO ledger (id, owner_id, reservation_id, service, feature, model, input_tokens, output_tokens,
                    cache_read_tokens, cache_write_tokens, cost_micros, request_id, occurred_at)
VALUES (@id, @owner_id, @reservation_id, @service, @feature, @model, @input_tokens, @output_tokens,
        @cache_read_tokens, @cache_write_tokens, @cost_micros, @request_id, @occurred_at)
RETURNING *;

-- name: GetLedgerEntryByReservation :one
SELECT * FROM ledger WHERE reservation_id = @reservation_id;
