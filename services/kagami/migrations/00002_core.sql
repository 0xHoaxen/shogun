-- +goose Up
-- Core kagami tables, as in docs/shogun-ddl.md. citext resolves through the
-- extensions schema on the service role's search_path.

CREATE TABLE companies (
    id          uuid PRIMARY KEY,
    owner_id    uuid NOT NULL,
    name        text NOT NULL,
    domain      text,
    notes       text,
    version     int  NOT NULL DEFAULT 1,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    archived_at timestamptz
);
CREATE UNIQUE INDEX companies_domain ON companies (owner_id, lower(domain)) WHERE domain IS NOT NULL;
CREATE INDEX companies_name ON companies (owner_id, lower(name));

CREATE TABLE jobs (
    id              uuid PRIMARY KEY,
    owner_id        uuid NOT NULL,
    company_id      uuid NOT NULL REFERENCES companies (id),
    title           text NOT NULL,
    url             text,
    source          text NOT NULL DEFAULT 'manual', -- manual | discovery | mail | import
    status          text NOT NULL DEFAULT 'saved'
                    CHECK (status IN ('saved', 'applied', 'shortlisted', 'interview', 'offer', 'rejected')),
    applied_on      date,
    next_follow_up  date,
    location        text,
    salary_text     text,
    description     text,
    idempotency_key text,
    version         int  NOT NULL DEFAULT 1,
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now(),
    archived_at     timestamptz
);
CREATE UNIQUE INDEX jobs_url ON jobs (owner_id, url) WHERE url IS NOT NULL;
CREATE UNIQUE INDEX jobs_idem ON jobs (owner_id, idempotency_key) WHERE idempotency_key IS NOT NULL;
CREATE INDEX jobs_status ON jobs (owner_id, status, updated_at DESC) WHERE archived_at IS NULL;
CREATE INDEX jobs_follow_up ON jobs (next_follow_up) WHERE next_follow_up IS NOT NULL AND archived_at IS NULL;

CREATE TABLE job_events (
    id              uuid PRIMARY KEY,
    job_id          uuid NOT NULL REFERENCES jobs (id),
    kind            text NOT NULL CHECK (kind IN ('created', 'status_changed', 'note', 'mail_linked', 'follow_up_set')),
    from_status     text,
    to_status       text,
    source_event_id uuid, -- the event that caused it, if any
    payload         jsonb NOT NULL DEFAULT '{}',
    occurred_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX job_events_job ON job_events (job_id, occurred_at DESC);

CREATE TABLE contacts (
    id                uuid PRIMARY KEY,
    owner_id          uuid NOT NULL,
    full_name         text NOT NULL,
    company_id        uuid REFERENCES companies (id),
    role              text,
    email             citext,
    linkedin_url      text,
    x_handle          text,
    phone             text,
    relationship      text, -- recruiter | engineer | manager | alumni | friend | other
    how_we_met        text,
    status            text NOT NULL DEFAULT 'not_reached'
                      CHECK (status IN ('not_reached', 'reached_out', 'conversation_started', 'replied', 'referral_asked')),
    preferred_channel text CHECK (preferred_channel IN ('email', 'linkedin', 'x', 'phone', 'other')),
    last_contacted    date,
    next_follow_up    date,
    target_role       text,
    job_id            uuid REFERENCES jobs (id), -- from the CSV job_link column
    tags              text[] NOT NULL DEFAULT '{}',
    notes             text,
    idempotency_key   text,
    version           int  NOT NULL DEFAULT 1,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),
    archived_at       timestamptz
);
CREATE UNIQUE INDEX contacts_email ON contacts (owner_id, email) WHERE email IS NOT NULL;
CREATE UNIQUE INDEX contacts_linkedin ON contacts (owner_id, linkedin_url) WHERE linkedin_url IS NOT NULL;
CREATE INDEX contacts_status ON contacts (owner_id, status) WHERE archived_at IS NULL;
CREATE INDEX contacts_follow_up ON contacts (next_follow_up) WHERE next_follow_up IS NOT NULL AND archived_at IS NULL;
CREATE INDEX contacts_tags ON contacts USING gin (tags);

CREATE TABLE contact_events (
    id              uuid PRIMARY KEY,
    contact_id      uuid NOT NULL REFERENCES contacts (id),
    kind            text NOT NULL CHECK (kind IN ('created', 'status_changed', 'message_sent', 'reply_received', 'note')),
    channel         text,
    from_status     text,
    to_status       text,
    source_event_id uuid,
    payload         jsonb NOT NULL DEFAULT '{}', -- draft_id, message_id, ...
    occurred_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX contact_events_contact ON contact_events (contact_id, occurred_at DESC);

CREATE TABLE imports (
    id           uuid PRIMARY KEY,
    owner_id     uuid NOT NULL,
    filename     text NOT NULL,
    rows_total   int  NOT NULL,
    rows_created int  NOT NULL,
    rows_updated int  NOT NULL,
    rows_failed  int  NOT NULL,
    errors       jsonb NOT NULL DEFAULT '[]', -- [{row, column, message}]
    created_at   timestamptz NOT NULL DEFAULT now()
);

-- +goose Down
DROP TABLE imports;
DROP TABLE contact_events;
DROP TABLE contacts;
DROP TABLE job_events;
DROP TABLE jobs;
DROP TABLE companies;
