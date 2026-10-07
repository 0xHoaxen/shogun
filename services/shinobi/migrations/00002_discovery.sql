-- +goose Up
CREATE TABLE sources (
    id          uuid PRIMARY KEY,
    owner_id    uuid NOT NULL,
    name        text NOT NULL,
    kind        text NOT NULL CHECK (kind IN ('api', 'rss', 'file')),
    config      jsonb NOT NULL,
    schedule    text NOT NULL DEFAULT '0 7 * * *',
    enabled     boolean NOT NULL DEFAULT true,
    last_run_at timestamptz,
    last_error  text
);

CREATE INDEX sources_owner ON sources (owner_id, name);

CREATE TABLE postings (
    id           uuid PRIMARY KEY,
    owner_id     uuid NOT NULL,
    source_id    uuid NOT NULL REFERENCES sources (id),
    external_id  text NOT NULL,
    title        text NOT NULL,
    company      text,
    url          text,
    location     text,
    posted_at    timestamptz,
    raw          jsonb NOT NULL,
    saved_job_id uuid,
    created_at   timestamptz NOT NULL DEFAULT now(),
    UNIQUE (source_id, external_id)
);

CREATE INDEX postings_owner ON postings (owner_id, id DESC);

CREATE TABLE preferences (
    owner_id     uuid PRIMARY KEY,
    roles        text[] NOT NULL DEFAULT '{}',
    locations    text[] NOT NULL DEFAULT '{}',
    must_have    text[] NOT NULL DEFAULT '{}',
    nice_to_have text[] NOT NULL DEFAULT '{}',
    exclude      text[] NOT NULL DEFAULT '{}',
    min_score    real NOT NULL DEFAULT 0.7 CHECK (min_score BETWEEN 0 AND 1),
    updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE scores (
    posting_id uuid PRIMARY KEY REFERENCES postings (id),
    score      real NOT NULL CHECK (score BETWEEN 0 AND 1),
    reasons    jsonb NOT NULL,
    scored_by  text NOT NULL CHECK (scored_by IN ('rule', 'llm')),
    created_at timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX scores_top ON scores (score DESC);

-- +goose Down
DROP TABLE scores;
DROP TABLE preferences;
DROP TABLE postings;
DROP TABLE sources;
