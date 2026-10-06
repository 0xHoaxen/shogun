-- +goose Up
-- Core fude tables, as in docs/shogun-ddl.md. citext and vector resolve
-- through the extensions schema on the service role's search_path.

CREATE TABLE drafts (
    id              uuid PRIMARY KEY,
    owner_id        uuid NOT NULL,
    kind            text NOT NULL CHECK (kind IN ('cover_letter', 'outreach', 'follow_up', 'post', 'one_off')),
    target_type     text NOT NULL CHECK (target_type IN ('job', 'contact', 'learning_activity', 'none')),
    target_id       uuid,                     -- id in kagami or dojo, no FK
    channel         text NOT NULL CHECK (channel IN ('email', 'linkedin', 'x', 'other')),
    state           text NOT NULL DEFAULT 'generating'
                    CHECK (state IN ('generating', 'pending', 'approved', 'sent', 'discarded', 'failed')),
    current_version int  NOT NULL DEFAULT 0,
    recipient       citext,                   -- email address when channel = email
    failure_reason  text,
    version         int  NOT NULL DEFAULT 1,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX drafts_queue ON drafts (owner_id, state, updated_at DESC);
CREATE INDEX drafts_target ON drafts (target_type, target_id);

CREATE TABLE draft_versions (
    draft_id        uuid NOT NULL REFERENCES drafts (id),
    version         int  NOT NULL,
    subject         text,
    body            text NOT NULL,
    body_sha256     bytea NOT NULL,
    extra_context   text,                     -- what the owner typed when asking to regenerate
    prompt_context  jsonb NOT NULL DEFAULT '{}',
    model           text,
    reservation_id  uuid,                     -- soroban reservation, no FK
    created_by      text NOT NULL CHECK (created_by IN ('ai', 'user')),
    created_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (draft_id, version)
);

CREATE TABLE approvals (
    id           uuid PRIMARY KEY,
    draft_id     uuid NOT NULL REFERENCES drafts (id),
    version      int  NOT NULL,
    token_jti    uuid NOT NULL UNIQUE,
    approved_by  uuid NOT NULL,
    approved_at  timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL
);

CREATE TABLE voice_samples (
    id          uuid PRIMARY KEY,
    owner_id    uuid NOT NULL,
    channel     text NOT NULL CHECK (channel IN ('email', 'linkedin', 'x', 'other')),
    text        text NOT NULL,
    embedding   vector(1024),
    created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX voice_samples_embedding ON voice_samples USING hnsw (embedding vector_cosine_ops);

CREATE TABLE templates (
    id             uuid PRIMARY KEY,
    owner_id       uuid NOT NULL,
    kind           text NOT NULL,
    contact_status text,                      -- for outreach: which status triggers it
    channel        text NOT NULL,
    instructions   text NOT NULL,
    version        int  NOT NULL DEFAULT 1,
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE NULLS NOT DISTINCT (owner_id, kind, contact_status, channel)
);

-- +goose Down
DROP TABLE templates;
DROP TABLE voice_samples;
DROP TABLE approvals;
DROP TABLE draft_versions;
DROP TABLE drafts;
