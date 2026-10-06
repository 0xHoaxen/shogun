# Table structures

Full Postgres 17 DDL for every schema, as the first migration of each service will create it. Each block runs as that service's own role with `search_path` set to its schema.

**Conventions.** IDs are `uuid` (UUIDv7 from Go). Money is `bigint` micro-dollars. Enums are `text` with `CHECK`. Every user-facing table has `owner_id`, `version` (optimistic locking), `created_at`, `updated_at` and `archived_at`. Every foreign key stays inside its own schema; cross-service references (like `fude.drafts.target_id`) are plain `uuid` with no constraint.

## Bootstrap (run once by the platform, not by a service)

```sql
-- One schema and one login role per service; a role sees only its own schema.
CREATE EXTENSION IF NOT EXISTS vector;     -- pgvector, used by fude
CREATE EXTENSION IF NOT EXISTS citext;     -- case-insensitive emails
-- repeated for: torii kagami tsubame fude taiko dojo katana shinobi sensei soroban
CREATE ROLE kagami LOGIN PASSWORD :'kagami_password';
CREATE SCHEMA kagami AUTHORIZATION kagami;
REVOKE ALL ON SCHEMA public FROM kagami;
ALTER ROLE kagami SET search_path = kagami;
```

## Shared tables (in every schema)

```sql
CREATE TABLE outbox (
  id            uuid PRIMARY KEY,
  type          text        NOT NULL,           -- e.g. 'job.added'
  subject       uuid        NOT NULL,           -- aggregate id
  body          bytea       NOT NULL,           -- shogun.events.v1.Envelope
  created_at    timestamptz NOT NULL DEFAULT now(),
  delivered_at  timestamptz
);
CREATE INDEX outbox_pending ON outbox (created_at) WHERE delivered_at IS NULL;

CREATE TABLE inbox (
  event_id      uuid PRIMARY KEY,
  type          text        NOT NULL,
  received_at   timestamptz NOT NULL DEFAULT now(),
  processed_at  timestamptz
);

-- river_job, river_leader, river_queue, river_migration: created by River's own migrator.
```

## torii

```sql
CREATE TABLE sessions (
  id            uuid PRIMARY KEY,
  owner_id      uuid        NOT NULL,
  token_hash    bytea       NOT NULL UNIQUE,    -- sha256 of the cookie value
  user_email    citext      NOT NULL,
  user_agent    text,
  ip            inet,
  created_at    timestamptz NOT NULL DEFAULT now(),
  last_seen_at  timestamptz NOT NULL DEFAULT now(),
  expires_at    timestamptz NOT NULL
);
CREATE INDEX sessions_expiry ON sessions (expires_at);
```

## kagami

```sql
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
  company_id      uuid NOT NULL REFERENCES companies(id),
  title           text NOT NULL,
  url             text,
  source          text NOT NULL DEFAULT 'manual',   -- manual | discovery | mail | import
  status          text NOT NULL DEFAULT 'saved'
                  CHECK (status IN ('saved','applied','shortlisted','interview','offer','rejected')),
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
  id          uuid PRIMARY KEY,
  job_id      uuid NOT NULL REFERENCES jobs(id),
  kind        text NOT NULL CHECK (kind IN ('created','status_changed','note','mail_linked','follow_up_set')),
  from_status text,
  to_status   text,
  source_event_id uuid,                       -- the event that caused it, if any
  payload     jsonb NOT NULL DEFAULT '{}',
  occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX job_events_job ON job_events (job_id, occurred_at DESC);

CREATE TABLE contacts (
  id                uuid PRIMARY KEY,
  owner_id          uuid NOT NULL,
  full_name         text NOT NULL,
  company_id        uuid REFERENCES companies(id),
  role              text,
  email             citext,
  linkedin_url      text,
  x_handle          text,
  phone             text,
  relationship      text,                     -- recruiter | engineer | manager | alumni | friend | other
  how_we_met        text,
  status            text NOT NULL DEFAULT 'not_reached'
                    CHECK (status IN ('not_reached','reached_out','conversation_started','replied','referral_asked')),
  preferred_channel text CHECK (preferred_channel IN ('email','linkedin','x','phone','other')),
  last_contacted    date,
  next_follow_up    date,
  target_role       text,
  job_id            uuid REFERENCES jobs(id),  -- from the CSV job_link column
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
  id          uuid PRIMARY KEY,
  contact_id  uuid NOT NULL REFERENCES contacts(id),
  kind        text NOT NULL CHECK (kind IN ('created','status_changed','message_sent','reply_received','note')),
  channel     text,
  from_status text,
  to_status   text,
  source_event_id uuid,
  payload     jsonb NOT NULL DEFAULT '{}',    -- draft_id, message_id, ...
  occurred_at timestamptz NOT NULL DEFAULT now()
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
  errors       jsonb NOT NULL DEFAULT '[]',  -- [{row, column, message}]
  created_at   timestamptz NOT NULL DEFAULT now()
);
```

## tsubame

```sql
CREATE TABLE accounts (
  id               uuid PRIMARY KEY,
  owner_id         uuid NOT NULL,
  provider         text NOT NULL CHECK (provider IN ('gmail')),   -- modular: add 'outlook' later
  address          citext NOT NULL,
  token_ciphertext bytea NOT NULL,          -- envelope-encrypted OAuth refresh token
  token_key_id     text  NOT NULL,
  history_id       text,                    -- Gmail incremental sync cursor
  status           text NOT NULL DEFAULT 'active' CHECK (status IN ('active','reauth_required','disabled')),
  last_synced_at   timestamptz,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  UNIQUE (owner_id, provider, address)
);

CREATE TABLE messages (
  id                  uuid PRIMARY KEY,
  owner_id            uuid NOT NULL,
  account_id          uuid NOT NULL REFERENCES accounts(id),
  provider_message_id text NOT NULL,
  provider_thread_id  text,
  direction           text NOT NULL CHECK (direction IN ('inbound','outbound')),
  from_addr           citext NOT NULL,
  to_addrs            citext[] NOT NULL DEFAULT '{}',
  subject             text,
  snippet             text,
  received_at         timestamptz NOT NULL,
  classification      text CHECK (classification IN ('application_confirmation','interview_invite','rejection',
                                  'offer','recruiter_outreach','reply','other')),
  confidence          real,
  classified_by       text CHECK (classified_by IN ('rule','llm','user')),
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
  account_id          uuid NOT NULL REFERENCES accounts(id),
  draft_id            uuid NOT NULL,        -- fude id, no FK
  draft_version       int  NOT NULL,
  contact_id          uuid,                 -- kagami id, no FK; echoed in draft.sent
  job_id              uuid,                 -- kagami id, no FK; echoed in draft.sent
  token_jti           uuid NOT NULL UNIQUE, -- makes each Hanko single use
  to_addrs            citext[] NOT NULL,
  provider_message_id text,
  status              text NOT NULL CHECK (status IN ('sending','sent','failed')),
  error               text,
  created_at          timestamptz NOT NULL DEFAULT now(),
  sent_at             timestamptz
);
```

## fude

```sql
CREATE TABLE drafts (
  id              uuid PRIMARY KEY,
  owner_id        uuid NOT NULL,
  kind            text NOT NULL CHECK (kind IN ('cover_letter','outreach','follow_up','post','one_off')),
  target_type     text NOT NULL CHECK (target_type IN ('job','contact','learning_activity','none')),
  target_id       uuid,                     -- id in kagami or dojo, no FK
  channel         text NOT NULL CHECK (channel IN ('email','linkedin','x','other')),
  state           text NOT NULL DEFAULT 'generating'
                  CHECK (state IN ('generating','pending','approved','sent','discarded','failed')),
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
  draft_id        uuid NOT NULL REFERENCES drafts(id),
  version         int  NOT NULL,
  subject         text,
  body            text NOT NULL,
  body_sha256     bytea NOT NULL,
  extra_context   text,                     -- what you typed when asking to regenerate
  prompt_context  jsonb NOT NULL DEFAULT '{}',
  model           text,
  reservation_id  uuid,                     -- soroban reservation, no FK
  created_by      text NOT NULL CHECK (created_by IN ('ai','user')),
  created_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (draft_id, version)
);

CREATE TABLE approvals (
  id           uuid PRIMARY KEY,
  draft_id     uuid NOT NULL REFERENCES drafts(id),
  version      int  NOT NULL,
  token_jti    uuid NOT NULL UNIQUE,
  approved_by  uuid NOT NULL,
  approved_at  timestamptz NOT NULL DEFAULT now(),
  expires_at   timestamptz NOT NULL
);

CREATE TABLE voice_samples (
  id          uuid PRIMARY KEY,
  owner_id    uuid NOT NULL,
  channel     text NOT NULL CHECK (channel IN ('email','linkedin','x','other')),
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
  UNIQUE (owner_id, kind, contact_status, channel)
);
```

## taiko

```sql
CREATE TABLE notifications (
  id              uuid PRIMARY KEY,
  owner_id        uuid NOT NULL,
  type            text NOT NULL,            -- draft_ready, interview_invite, budget_80, ...
  title           text NOT NULL,
  body            text,
  link            text,                     -- in-app route, e.g. /drafts/<id>
  source_event_id uuid UNIQUE,              -- one notification per event
  read_at         timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notifications_unread ON notifications (owner_id, created_at DESC) WHERE read_at IS NULL;

CREATE TABLE channel_settings (
  owner_id    uuid NOT NULL,
  channel     text NOT NULL CHECK (channel IN ('in_app','email_digest')),
  enabled     boolean NOT NULL DEFAULT true,
  quiet_from  time,
  quiet_to    time,
  PRIMARY KEY (owner_id, channel)
);
```

## dojo

```sql
CREATE TABLE items (
  id           uuid PRIMARY KEY,
  owner_id     uuid NOT NULL,
  title        text NOT NULL,
  kind         text NOT NULL CHECK (kind IN ('course','book','project','skill')),
  url          text,
  status       text NOT NULL DEFAULT 'planned' CHECK (status IN ('planned','in_progress','done')),
  started_on   date,
  completed_on date,
  insight      text,                        -- what you took away, used for posts
  version      int  NOT NULL DEFAULT 1,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  archived_at  timestamptz
);

CREATE TABLE activities (
  id          uuid PRIMARY KEY,
  owner_id    uuid NOT NULL,
  item_id     uuid REFERENCES items(id),
  summary     text NOT NULL,
  minutes     int,
  occurred_on date NOT NULL DEFAULT current_date,
  tags        text[] NOT NULL DEFAULT '{}',
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX activities_item ON activities (item_id, occurred_on DESC);
```

## katana

```sql
CREATE TABLE github_snapshots (
  id            uuid PRIMARY KEY,
  owner_id      uuid NOT NULL,
  taken_at      timestamptz NOT NULL DEFAULT now(),
  repos         jsonb NOT NULL,             -- name, languages, stars, last push
  contributions jsonb NOT NULL,             -- merged PRs, releases, commit counts by repo
  etag          text
);

CREATE TABLE suggestions (
  id          uuid PRIMARY KEY,
  owner_id    uuid NOT NULL,
  target      text NOT NULL CHECK (target IN ('resume','linkedin')),
  section     text NOT NULL,                -- headline, about, experience, skills, projects
  before      text,
  after       text NOT NULL,
  reason      text NOT NULL,
  evidence    jsonb NOT NULL DEFAULT '[]',  -- links to PRs, repos, learning items
  state       text NOT NULL DEFAULT 'open' CHECK (state IN ('open','accepted','dismissed')),
  created_at  timestamptz NOT NULL DEFAULT now(),
  decided_at  timestamptz
);
CREATE INDEX suggestions_open ON suggestions (owner_id, created_at DESC) WHERE state = 'open';
```

## shinobi

```sql
CREATE TABLE sources (
  id          uuid PRIMARY KEY,
  owner_id    uuid NOT NULL,
  name        text NOT NULL,
  kind        text NOT NULL CHECK (kind IN ('api','rss','file')),
  config      jsonb NOT NULL,               -- url, auth ref, field mapping
  schedule    text NOT NULL DEFAULT '0 7 * * *',
  enabled     boolean NOT NULL DEFAULT true,
  last_run_at timestamptz,
  last_error  text
);

CREATE TABLE postings (
  id          uuid PRIMARY KEY,
  owner_id    uuid NOT NULL,
  source_id   uuid NOT NULL REFERENCES sources(id),
  external_id text NOT NULL,
  title       text NOT NULL,
  company     text,
  url         text,
  location    text,
  posted_at   timestamptz,
  raw         jsonb NOT NULL,
  saved_job_id uuid,                        -- kagami id once saved, no FK
  created_at  timestamptz NOT NULL DEFAULT now(),
  UNIQUE (source_id, external_id)
);

CREATE TABLE preferences (
  owner_id     uuid PRIMARY KEY,
  roles        text[] NOT NULL DEFAULT '{}',
  locations    text[] NOT NULL DEFAULT '{}',
  must_have    text[] NOT NULL DEFAULT '{}',
  nice_to_have text[] NOT NULL DEFAULT '{}',
  exclude      text[] NOT NULL DEFAULT '{}',
  min_score    real NOT NULL DEFAULT 0.7,
  updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE scores (
  posting_id  uuid PRIMARY KEY REFERENCES postings(id),
  score       real NOT NULL CHECK (score BETWEEN 0 AND 1),
  reasons     jsonb NOT NULL,
  scored_by   text NOT NULL CHECK (scored_by IN ('rule','llm')),
  created_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX scores_top ON scores (score DESC);
```

## sensei

```sql
CREATE TABLE facts (
  event_id    uuid PRIMARY KEY,             -- one fact per consumed event
  owner_id    uuid NOT NULL,
  type        text NOT NULL,
  dimension   jsonb NOT NULL,               -- source, channel, status, company ...
  occurred_at timestamptz NOT NULL
);
CREATE INDEX facts_type_time ON facts (owner_id, type, occurred_at);

CREATE TABLE daily_rollups (
  owner_id   uuid NOT NULL,
  day        date NOT NULL,
  metric     text NOT NULL,                 -- applications, interviews, outreach_sent, replies, ...
  dimension  text NOT NULL DEFAULT '',      -- e.g. 'source=linkedin'
  value      bigint NOT NULL,
  PRIMARY KEY (owner_id, day, metric, dimension)
);
```

## soroban

```sql
CREATE TABLE prices (
  model                     text NOT NULL,
  effective_from            date NOT NULL,
  input_micros_per_mtok     bigint NOT NULL,   -- micro-dollars per million tokens
  output_micros_per_mtok    bigint NOT NULL,
  cache_read_micros_per_mtok  bigint NOT NULL DEFAULT 0,
  cache_write_micros_per_mtok bigint NOT NULL DEFAULT 0,
  PRIMARY KEY (model, effective_from)
);

CREATE TABLE budgets (
  id           uuid PRIMARY KEY,
  owner_id     uuid NOT NULL,
  scope_type   text NOT NULL CHECK (scope_type IN ('global','service','feature')),
  scope_value  text NOT NULL DEFAULT '',     -- '' | 'fude' | 'fude.cover_letter'
  period       text NOT NULL CHECK (period IN ('daily','monthly')),
  limit_micros bigint NOT NULL CHECK (limit_micros >= 0),
  mode         text NOT NULL CHECK (mode IN ('hard','soft')),
  thresholds   int[] NOT NULL DEFAULT '{50,80,100}',
  enabled      boolean NOT NULL DEFAULT true,
  version      int NOT NULL DEFAULT 1,
  updated_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (owner_id, scope_type, scope_value, period)
);

CREATE TABLE budget_periods (
  id                  uuid PRIMARY KEY,
  budget_id           uuid NOT NULL REFERENCES budgets(id),
  period_start        timestamptz NOT NULL,
  period_end          timestamptz NOT NULL,
  spent_micros        bigint NOT NULL DEFAULT 0,
  reserved_micros     bigint NOT NULL DEFAULT 0,
  notified_thresholds int[]  NOT NULL DEFAULT '{}',
  UNIQUE (budget_id, period_start)
);

CREATE TABLE reservations (
  id                uuid PRIMARY KEY,
  owner_id          uuid NOT NULL,
  service           text NOT NULL,
  feature           text NOT NULL,
  model             text NOT NULL,
  est_micros        bigint NOT NULL,
  budget_period_ids uuid[] NOT NULL,
  status            text NOT NULL DEFAULT 'open' CHECK (status IN ('open','committed','released','expired')),
  created_at        timestamptz NOT NULL DEFAULT now(),
  expires_at        timestamptz NOT NULL
);
CREATE INDEX reservations_open ON reservations (expires_at) WHERE status = 'open';

CREATE TABLE ledger (
  id                 uuid PRIMARY KEY,
  owner_id           uuid NOT NULL,
  reservation_id     uuid NOT NULL UNIQUE REFERENCES reservations(id),
  service            text NOT NULL,
  feature            text NOT NULL,
  model              text NOT NULL,
  input_tokens       int  NOT NULL,
  output_tokens      int  NOT NULL,
  cache_read_tokens  int  NOT NULL DEFAULT 0,
  cache_write_tokens int  NOT NULL DEFAULT 0,
  cost_micros        bigint NOT NULL,
  request_id         text,
  occurred_at        timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ledger_time ON ledger (owner_id, occurred_at);
CREATE INDEX ledger_feature ON ledger (owner_id, feature, occurred_at);
```
