-- name: GetPreferences :one
SELECT * FROM preferences WHERE owner_id = @owner_id;

-- name: UpsertPreferences :one
INSERT INTO preferences (owner_id, roles, locations, must_have, nice_to_have, exclude, min_score, updated_at)
VALUES (@owner_id, @roles, @locations, @must_have, @nice_to_have, @exclude, @min_score, @updated_at)
ON CONFLICT (owner_id) DO UPDATE SET
    roles = EXCLUDED.roles, locations = EXCLUDED.locations, must_have = EXCLUDED.must_have,
    nice_to_have = EXCLUDED.nice_to_have, exclude = EXCLUDED.exclude, min_score = EXCLUDED.min_score,
    updated_at = EXCLUDED.updated_at
RETURNING *;
