-- name: InsertMessage :one
-- A message already stored (same account and provider id) returns no row.
INSERT INTO messages (
    id, owner_id, account_id, provider_message_id, provider_thread_id, direction,
    from_addr, to_addrs, subject, snippet, received_at
) VALUES (
    @id, @owner_id, @account_id, @provider_message_id, @provider_thread_id, @direction,
    @from_addr, @to_addrs, @subject, @snippet, @received_at
)
ON CONFLICT (account_id, provider_message_id) DO NOTHING
RETURNING *;

-- name: GetMessage :one
SELECT * FROM messages WHERE id = @id AND owner_id = @owner_id;

-- name: SetMessageClassification :execrows
-- Classifies a message once; a message already classified is left alone.
UPDATE messages SET
    classification = @classification,
    confidence = @confidence,
    classified_by = @classified_by,
    linked_job_id = @linked_job_id,
    linked_contact_id = @linked_contact_id
WHERE id = @id AND owner_id = @owner_id AND classification IS NULL;

-- name: ThreadHasOutbound :one
-- Whether the owner sent anything in the thread, which makes mail in it a
-- reply.
SELECT EXISTS (
    SELECT 1 FROM messages
    WHERE account_id = @account_id AND provider_thread_id = @thread_id AND direction = 'outbound'
);

-- name: ThreadLinks :one
-- The job and contact an earlier message of the thread was linked to.
SELECT linked_job_id, linked_contact_id FROM messages
WHERE account_id = @account_id AND provider_thread_id = @thread_id AND id <> @id
  AND (linked_job_id IS NOT NULL OR linked_contact_id IS NOT NULL)
ORDER BY received_at DESC, id DESC
LIMIT 1;
