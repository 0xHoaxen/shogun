-- name: InsertVoiceSample :one
INSERT INTO voice_samples (id, owner_id, channel, text)
VALUES (@id, @owner_id, @channel, @text)
RETURNING id, owner_id, channel, text, created_at;

-- name: ListRecentVoiceSamples :many
-- The owner's newest samples for a channel, used when no embedding is there to
-- rank them by similarity.
SELECT id, owner_id, channel, text, created_at FROM voice_samples
WHERE owner_id = @owner_id AND channel = @channel
ORDER BY created_at DESC, id DESC
LIMIT @row_limit;

-- name: ListSimilarVoiceSamples :many
-- The owner's samples for a channel closest to a query embedding. Samples with
-- no embedding yet come last, newest first, so a fresh sample still counts.
SELECT id, owner_id, channel, text, created_at FROM voice_samples
WHERE owner_id = @owner_id AND channel = @channel
ORDER BY embedding <=> @query::vector NULLS LAST, created_at DESC, id DESC
LIMIT @row_limit;

-- name: GetVoiceSampleText :one
SELECT text FROM voice_samples WHERE id = @id;

-- name: SetVoiceSampleEmbedding :execrows
UPDATE voice_samples SET embedding = @embedding::vector WHERE id = @id;
