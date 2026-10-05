-- name: GetPriceOn :one
-- The price in force on a date: the latest one effective on or before it.
SELECT * FROM prices
WHERE model = @model AND effective_from <= @on_date
ORDER BY effective_from DESC
LIMIT 1;
