-- name: DeleteRollupsFrom :exec
-- The owner's rollups from from_day on; they are recomputed from the facts next.
DELETE FROM daily_rollups WHERE owner_id = @owner_id AND day >= @from_day;

-- name: RollupJobsAdded :exec
-- Jobs added per day and source.
INSERT INTO daily_rollups (owner_id, day, metric, dimension, value)
SELECT f.owner_id, (f.occurred_at AT TIME ZONE @tz::text)::date, 'jobs_added',
       'source=' || COALESCE(f.dimension->>'source', 'unknown'), count(*)
FROM facts f
WHERE f.owner_id = @owner_id AND f.type = 'job.added' AND (f.occurred_at AT TIME ZONE @tz::text)::date >= @from_day
GROUP BY 1, 2, 3, 4;

-- name: RollupJobStages :exec
-- Moves into each stage per day, by the source the job was added from; a job
-- sensei never saw added counts under source=unknown.
INSERT INTO daily_rollups (owner_id, day, metric, dimension, value)
SELECT f.owner_id, (f.occurred_at AT TIME ZONE @tz::text)::date,
       CASE f.dimension->>'to'
         WHEN 'applied' THEN 'applications' WHEN 'shortlisted' THEN 'shortlisted'
         WHEN 'interview' THEN 'interviews' WHEN 'offer' THEN 'offers' ELSE 'rejections' END,
       'source=' || COALESCE(a.source, 'unknown'), count(*)
FROM facts f
LEFT JOIN LATERAL (
    SELECT added.dimension->>'source' AS source FROM facts added
    WHERE added.type = 'job.added' AND added.owner_id = f.owner_id
      AND added.dimension->>'job_id' = f.dimension->>'job_id'
    ORDER BY added.occurred_at LIMIT 1
) a ON true
WHERE f.owner_id = @owner_id AND f.type = 'job.status_changed'
  AND f.dimension->>'to' IN ('applied', 'shortlisted', 'interview', 'offer', 'rejected')
  AND (f.occurred_at AT TIME ZONE @tz::text)::date >= @from_day
GROUP BY 1, 2, 3, 4;

-- name: RollupContacts :exec
-- First contacts and replies per day and channel, and moves per day and status.
INSERT INTO daily_rollups (owner_id, day, metric, dimension, value)
SELECT f.owner_id, (f.occurred_at AT TIME ZONE @tz::text)::date,
       CASE f.dimension->>'to' WHEN 'reached_out' THEN 'outreach_sent' ELSE 'replies' END,
       'channel=' || COALESCE(f.dimension->>'channel', 'unknown'), count(*)
FROM facts f
WHERE f.owner_id = @owner_id AND f.type = 'contact.status_changed' AND f.dimension->>'to' IN ('reached_out', 'replied')
  AND (f.occurred_at AT TIME ZONE @tz::text)::date >= @from_day
GROUP BY 1, 2, 3, 4;

-- name: RollupContactMoves :exec
INSERT INTO daily_rollups (owner_id, day, metric, dimension, value)
SELECT f.owner_id, (f.occurred_at AT TIME ZONE @tz::text)::date, 'contact_moves',
       'status=' || COALESCE(f.dimension->>'to', 'unknown'), count(*)
FROM facts f
WHERE f.owner_id = @owner_id AND f.type = 'contact.status_changed' AND (f.occurred_at AT TIME ZONE @tz::text)::date >= @from_day
GROUP BY 1, 2, 3, 4;

-- name: ListRollups :many
SELECT day, metric, dimension, value FROM daily_rollups
WHERE owner_id = @owner_id AND day BETWEEN @from_day AND @to_day AND metric = ANY(@metrics::text[])
ORDER BY day, metric, dimension;
