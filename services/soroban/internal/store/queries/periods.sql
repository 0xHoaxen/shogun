-- name: InsertPeriodIfAbsent :exec
INSERT INTO budget_periods (id, budget_id, period_start, period_end)
VALUES (@id, @budget_id, @period_start, @period_end)
ON CONFLICT (budget_id, period_start) DO NOTHING;

-- name: LockPeriod :one
SELECT * FROM budget_periods
WHERE budget_id = @budget_id AND period_start = @period_start
FOR UPDATE;

-- name: AddReserved :exec
UPDATE budget_periods SET reserved_micros = reserved_micros + @delta WHERE id = @id;

-- name: MarkThresholdNotified :exec
UPDATE budget_periods
SET notified_thresholds = array_append(notified_thresholds, @threshold::int)
WHERE id = @id AND NOT (@threshold::int = ANY (notified_thresholds));

-- name: LockPeriodsByID :many
-- Ordered by budget id, the order Reserve locks in, so the two cannot deadlock.
SELECT * FROM budget_periods WHERE id = ANY (@ids::uuid[]) ORDER BY budget_id FOR UPDATE;

-- name: SettlePeriod :exec
UPDATE budget_periods
SET reserved_micros = reserved_micros - @release_micros,
    spent_micros = spent_micros + @spend_micros
WHERE id = @id;

-- name: ListCurrentPeriods :many
SELECT bp.* FROM budget_periods bp
JOIN budgets b ON b.id = bp.budget_id
WHERE b.owner_id = @owner_id AND bp.period_start <= @now AND bp.period_end > @now;
