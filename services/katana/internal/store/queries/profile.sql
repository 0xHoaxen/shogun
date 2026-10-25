-- name: GetProfile :one
SELECT * FROM profiles WHERE owner_id = @owner_id;

-- name: InsertProfile :one
-- Returns no row when the owner already has a profile.
INSERT INTO profiles (owner_id, name, headline, location, email, years_experience, created_at, updated_at)
VALUES (@owner_id, @name, @headline, @location, @email, @years_experience, @now, @now)
ON CONFLICT (owner_id) DO NOTHING
RETURNING *;

-- name: UpdateProfile :one
-- Returns no row unless the stored version is the one the caller read.
UPDATE profiles
SET name = @name, headline = @headline, location = @location, email = @email,
    years_experience = @years_experience, version = version + 1, updated_at = @now
WHERE owner_id = @owner_id AND version = @version
RETURNING *;

-- name: DeleteProfileRoles :exec
DELETE FROM profile_roles WHERE owner_id = @owner_id;

-- name: InsertProfileRole :exec
INSERT INTO profile_roles (owner_id, position, title, company, start_month, end_month, is_current)
VALUES (@owner_id, @position, @title, @company, @start_month, @end_month, @is_current);

-- name: ListProfileRoles :many
SELECT * FROM profile_roles WHERE owner_id = @owner_id ORDER BY position;

-- name: UpsertSkill :exec
-- A skill that exists keeps its name and source; its level only goes up.
INSERT INTO skills (owner_id, name_key, name, level, source, created_at, updated_at)
VALUES (@owner_id, @name_key, @name, @level, @source, @now, @now)
ON CONFLICT (owner_id, name_key) DO UPDATE
SET level = GREATEST(skills.level, EXCLUDED.level),
    updated_at = CASE WHEN EXCLUDED.level > skills.level THEN EXCLUDED.updated_at ELSE skills.updated_at END;

-- name: ListSkills :many
SELECT * FROM skills WHERE owner_id = @owner_id ORDER BY level DESC, name_key;
