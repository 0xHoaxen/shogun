-- name: InsertDraftVersion :one
INSERT INTO draft_versions (
    draft_id, version, subject, body, body_sha256, extra_context, created_by
) VALUES (
    @draft_id, @version, @subject, @body, @body_sha256, @extra_context, @created_by
)
RETURNING *;

-- name: ListDraftVersions :many
SELECT * FROM draft_versions WHERE draft_id = @draft_id ORDER BY version DESC;
