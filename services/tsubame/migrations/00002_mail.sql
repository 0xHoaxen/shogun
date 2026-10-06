-- +goose Up
-- Core tsubame tables, as in docs/shogun-ddl.md. citext resolves through the
-- extensions schema on the service role's search_path.

CREATE TABLE accounts (
    id               uuid PRIMARY KEY,
    owner_id         uuid NOT NULL,
    provider         text NOT NULL CHECK (provider IN ('gmail')),   -- modular: add 'outlook' later
    address          citext NOT NULL,
    token_ciphertext bytea NOT NULL,          -- envelope-encrypted OAuth refresh token
    token_key_id     text  NOT NULL,
    history_id       text,                    -- Gmail incremental sync cursor
    status           text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'reauth_required', 'disabled')),
    last_synced_at   timestamptz,
    created_at       timestamptz NOT NULL DEFAULT now(),
    updated_at       timestamptz NOT NULL DEFAULT now(),
    UNIQUE (owner_id, provider, address)
);

CREATE TABLE messages (
    id                  uuid PRIMARY KEY,
    owner_id            uuid NOT NULL,
    account_id          uuid NOT NULL REFERENCES accounts (id),
    provider_message_id text NOT NULL,
    provider_thread_id  text,
    direction           text NOT NULL CHECK (direction IN ('inbound', 'outbound')),
    from_addr           citext NOT NULL,
    to_addrs            citext[] NOT NULL DEFAULT '{}',
    subject             text,
    snippet             text,
    received_at         timestamptz NOT NULL,
    classification      text CHECK (classification IN ('application_confirmation', 'interview_invite', 'rejection',
                                    'offer', 'recruiter_outreach', 'reply', 'other')),
    confidence          real,
    classified_by       text CHECK (classified_by IN ('rule', 'llm', 'user')),
    linked_job_id       uuid,                 -- kagami id, no FK
    linked_contact_id   uuid,                 -- kagami id, no FK
    created_at          timestamptz NOT NULL DEFAULT now(),
    UNIQUE (account_id, provider_message_id)
);
CREATE INDEX messages_class ON messages (owner_id, classification, received_at DESC);
CREATE INDEX messages_thread ON messages (account_id, provider_thread_id);

CREATE TABLE sends (
    id                  uuid PRIMARY KEY,
    owner_id            uuid NOT NULL,
    account_id          uuid NOT NULL REFERENCES accounts (id),
    draft_id            uuid NOT NULL,        -- fude id, no FK
    draft_version       int  NOT NULL,
    token_jti           uuid NOT NULL UNIQUE, -- makes each Hanko single use
    to_addrs            citext[] NOT NULL,
    provider_message_id text,
    status              text NOT NULL CHECK (status IN ('sending', 'sent', 'failed')),
    error               text,
    created_at          timestamptz NOT NULL DEFAULT now(),
    sent_at             timestamptz
);

-- +goose Down
DROP TABLE sends;
DROP TABLE messages;
DROP TABLE accounts;
