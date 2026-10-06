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
