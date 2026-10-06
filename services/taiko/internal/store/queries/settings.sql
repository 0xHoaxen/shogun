-- name: GetChannelSetting :one
SELECT * FROM channel_settings
WHERE owner_id = @owner_id AND channel = @channel;

-- name: UpsertChannelSetting :one
INSERT INTO channel_settings (owner_id, channel, enabled, quiet_from, quiet_to)
VALUES (@owner_id, @channel, @enabled, @quiet_from, @quiet_to)
ON CONFLICT (owner_id, channel) DO UPDATE SET
    enabled = EXCLUDED.enabled,
    quiet_from = EXCLUDED.quiet_from,
    quiet_to = EXCLUDED.quiet_to
RETURNING *;
