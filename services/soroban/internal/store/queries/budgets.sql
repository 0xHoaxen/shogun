-- name: HasBudgets :one
SELECT EXISTS (SELECT 1 FROM budgets WHERE owner_id = @owner_id);

-- name: ListBudgetTemplates :many
-- The defaults seeded by migration live under the nil owner id.
SELECT * FROM budgets WHERE owner_id = @template_owner_id ORDER BY id;

-- name: InsertBudgetIfAbsent :exec
INSERT INTO budgets (id, owner_id, scope_type, scope_value, period, limit_micros, mode, thresholds, enabled)
VALUES (@id, @owner_id, @scope_type, @scope_value, @period, @limit_micros, @mode, @thresholds, @enabled)
ON CONFLICT (owner_id, scope_type, scope_value, period) DO NOTHING;

-- name: ListMatchingBudgets :many
-- Enabled budgets a call is counted against: the global one, the service's
-- and the feature's. Ordered by id so that periods are always locked in the
-- same order and concurrent reservations cannot deadlock.
SELECT * FROM budgets
WHERE owner_id = @owner_id
  AND enabled
  AND (scope_type = 'global'
       OR (scope_type = 'service' AND scope_value = @service::text)
       OR (scope_type = 'feature' AND scope_value = @feature::text))
ORDER BY id;

-- name: ListBudgetsByID :many
SELECT * FROM budgets WHERE id = ANY (@ids::uuid[]);

-- name: ListBudgets :many
SELECT * FROM budgets WHERE owner_id = @owner_id ORDER BY scope_type, scope_value, period;

-- name: GetBudget :one
SELECT * FROM budgets WHERE id = @id AND owner_id = @owner_id;

-- name: InsertBudget :one
INSERT INTO budgets (id, owner_id, scope_type, scope_value, period, limit_micros, mode, thresholds, enabled)
VALUES (@id, @owner_id, @scope_type, @scope_value, @period, @limit_micros, @mode, @thresholds, @enabled)
RETURNING *;

-- name: UpdateBudget :one
-- Scope and period are the budget's identity and never change.
UPDATE budgets
SET limit_micros = @limit_micros, mode = @mode, thresholds = @thresholds, enabled = @enabled,
    version = version + 1, updated_at = now()
WHERE id = @id AND owner_id = @owner_id AND version = @version
RETURNING *;
