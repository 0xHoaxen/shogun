-- name: HasJobSkillRun :one
SELECT EXISTS (SELECT 1 FROM job_skill_runs WHERE owner_id = @owner_id AND job_id = @job_id);

-- name: InsertJobSkillRun :exec
INSERT INTO job_skill_runs (owner_id, job_id, created_at)
VALUES (@owner_id, @job_id, @now)
ON CONFLICT (owner_id, job_id) DO NOTHING;

-- name: InsertJobSkill :exec
INSERT INTO job_skills (owner_id, job_id, name_key, name)
VALUES (@owner_id, @job_id, @name_key, @name)
ON CONFLICT (owner_id, job_id, name_key) DO NOTHING;

-- name: CountJobSkillRuns :one
SELECT count(*) FROM job_skill_runs WHERE owner_id = @owner_id;

-- name: ListSkillsWithAsked :many
-- Every skill the owner has, and every skill a read job asked for that they do
-- not, with how many read jobs asked for it.
WITH asked AS (
    SELECT j.name_key, min(j.name) AS name, count(*)::int AS asked_in
    FROM job_skills j WHERE j.owner_id = @owner_id GROUP BY j.name_key
), have AS (
    SELECT k.name_key, k.name, k.level, k.source
    FROM skills k WHERE k.owner_id = @owner_id
)
SELECT COALESCE(h.name, a.name)::text AS name,
       COALESCE(h.level, 0)::smallint AS level,
       COALESCE(h.source, '')::text AS source,
       (h.name_key IS NOT NULL)::bool AS have,
       COALESCE(a.asked_in, 0)::int AS asked_in
FROM have h
FULL OUTER JOIN asked a ON a.name_key = h.name_key
ORDER BY COALESCE(a.asked_in, 0) DESC, COALESCE(h.level, 0) DESC, COALESCE(h.name_key, a.name_key);
