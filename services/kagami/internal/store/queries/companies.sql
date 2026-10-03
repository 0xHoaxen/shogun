-- name: GetCompany :one
SELECT * FROM companies WHERE id = @id AND owner_id = @owner_id;

-- name: UpsertCompanyByDomain :one
-- The domain is the upsert key; a repeat call returns the existing company.
INSERT INTO companies (id, owner_id, name, domain)
VALUES (@id, @owner_id, @name, @domain)
ON CONFLICT (owner_id, lower(domain)) WHERE domain IS NOT NULL
DO UPDATE SET updated_at = companies.updated_at
RETURNING *;

-- name: FindCompanyByName :one
SELECT * FROM companies
WHERE owner_id = @owner_id AND lower(name) = lower(@name::text) AND archived_at IS NULL
ORDER BY created_at
LIMIT 1;

-- name: InsertCompany :one
INSERT INTO companies (id, owner_id, name, domain)
VALUES (@id, @owner_id, @name, @domain)
RETURNING *;
