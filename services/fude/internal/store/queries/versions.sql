-- name: InsertDraftVersion :one
INSERT INTO draft_versions (
    draft_id, version, subject, body, body_sha256, extra_context, model, prompt_context, created_by
) VALUES (
    @draft_id, @version, @subject, @body, @body_sha256, @extra_context, @model,
    COALESCE(sqlc.narg(prompt_context)::jsonb, '{}'), @created_by
)
RETURNING *;

-- name: ListDraftVersions :many
SELECT * FROM draft_versions WHERE draft_id = @draft_id ORDER BY version DESC;

-- name: GetDraftVersion :one
SELECT * FROM draft_versions WHERE draft_id = @draft_id AND version = @version;
