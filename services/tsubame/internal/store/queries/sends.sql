-- name: InsertSend :one
-- The unique token_jti is what makes a Hanko single use: a second insert with
-- the same token fails.
INSERT INTO sends (
    id, owner_id, account_id, draft_id, draft_version, contact_id, job_id, token_jti, to_addrs, status
) VALUES (
    @id, @owner_id, @account_id, @draft_id, @draft_version, @contact_id, @job_id, @token_jti, @to_addrs, 'sending'
)
RETURNING *;

-- name: MarkSendSent :execrows
UPDATE sends SET status = 'sent', provider_message_id = @provider_message_id, sent_at = @sent_at, error = NULL
WHERE id = @id AND status = 'sending';

-- name: MarkSendFailed :execrows
UPDATE sends SET status = 'failed', error = @error
WHERE id = @id AND status = 'sending';

-- name: ListOpenSends :many
-- Sends that were started and never finished, oldest first.
SELECT * FROM sends WHERE status = 'sending' AND created_at <= @started_before ORDER BY created_at, id LIMIT @row_limit;
