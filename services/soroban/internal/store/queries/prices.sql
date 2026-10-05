-- name: GetPriceOn :one
-- The price in force on a date: the latest one effective on or before it.
SELECT * FROM prices
WHERE model = @model AND effective_from <= @on_date
ORDER BY effective_from DESC
LIMIT 1;

-- name: UpsertPrice :one
INSERT INTO prices (model, effective_from, input_micros_per_mtok, output_micros_per_mtok,
                    cache_read_micros_per_mtok, cache_write_micros_per_mtok)
VALUES (@model, @effective_from, @input_micros_per_mtok, @output_micros_per_mtok,
        @cache_read_micros_per_mtok, @cache_write_micros_per_mtok)
ON CONFLICT (model, effective_from) DO UPDATE SET
    input_micros_per_mtok = EXCLUDED.input_micros_per_mtok,
    output_micros_per_mtok = EXCLUDED.output_micros_per_mtok,
    cache_read_micros_per_mtok = EXCLUDED.cache_read_micros_per_mtok,
    cache_write_micros_per_mtok = EXCLUDED.cache_write_micros_per_mtok
RETURNING *;

-- name: ListPrices :many
SELECT * FROM prices ORDER BY model, effective_from DESC;
