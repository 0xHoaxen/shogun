-- +goose Up
-- Skills the owner's saved jobs ask for, read once per job by the model.
-- job_id is kagami's job id, a plain uuid with no foreign key. A run row marks
-- a job as read, so a job that asks for nothing still counts as read.
CREATE TABLE job_skill_runs (
    owner_id   uuid NOT NULL,
    job_id     uuid NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (owner_id, job_id)
);

CREATE TABLE job_skills (
    owner_id uuid NOT NULL,
    job_id   uuid NOT NULL,
    name_key text NOT NULL,
    name     text NOT NULL,
    PRIMARY KEY (owner_id, job_id, name_key)
);

-- +goose Down
DROP TABLE job_skills;
DROP TABLE job_skill_runs;
