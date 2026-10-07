-- name: GetChannelSetting :one
SELECT * FROM channel_settings
WHERE owner_id = @owner_id AND channel = @channel;

-- name: InsertChannelSetting :one
-- Returns no row when the owner already has a setting for the channel.
INSERT INTO channel_settings (owner_id, channel, enabled, quiet_from, quiet_to)
VALUES (@owner_id, @channel, @enabled, @quiet_from, @quiet_to)
ON CONFLICT (owner_id, channel) DO NOTHING
RETURNING *;

-- name: UpdateChannelSetting :one
-- Returns no row when the stored version is not @version.
UPDATE channel_settings SET
    enabled = @enabled,
    quiet_from = @quiet_from,
    quiet_to = @quiet_to,
    version = version + 1,
    updated_at = now()
WHERE owner_id = @owner_id AND channel = @channel AND version = @version
RETURNING *;

-- name: ListOwners :many
-- Every owner taiko has heard of: one with a notification or a setting.
SELECT owner_id FROM notifications
UNION
SELECT owner_id FROM channel_settings;
