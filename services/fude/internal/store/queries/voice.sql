-- name: InsertVoiceSample :one
INSERT INTO voice_samples (id, owner_id, channel, text)
VALUES (@id, @owner_id, @channel, @text)
RETURNING id, owner_id, channel, text, created_at;
