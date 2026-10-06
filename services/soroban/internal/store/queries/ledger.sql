-- name: InsertLedgerEntry :one
INSERT INTO ledger (id, owner_id, reservation_id, service, feature, model, input_tokens, output_tokens,
                    cache_read_tokens, cache_write_tokens, cost_micros, request_id, occurred_at)
VALUES (@id, @owner_id, @reservation_id, @service, @feature, @model, @input_tokens, @output_tokens,
        @cache_read_tokens, @cache_write_tokens, @cost_micros, @request_id, @occurred_at)
RETURNING *;

-- name: GetLedgerEntryByReservation :one
SELECT * FROM ledger WHERE reservation_id = @reservation_id;

-- name: SumSpend :many
-- group_by is service, feature, model or day; a day is an Asia/Kolkata date.
SELECT (CASE @group_by::text
          WHEN 'service' THEN service
          WHEN 'feature' THEN feature
          WHEN 'model' THEN model
          ELSE to_char(occurred_at AT TIME ZONE 'Asia/Kolkata', 'YYYY-MM-DD')
        END)::text AS key,
       sum(cost_micros)::bigint AS cost_micros,
       sum(input_tokens)::bigint AS input_tokens,
       sum(output_tokens)::bigint AS output_tokens,
       sum(cache_read_tokens)::bigint AS cache_read_tokens,
       sum(cache_write_tokens)::bigint AS cache_write_tokens
FROM ledger
WHERE owner_id = @owner_id AND occurred_at >= @from_time AND occurred_at < @to_time
GROUP BY 1
ORDER BY 1;
