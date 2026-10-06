-- +goose Up
-- A send remembers which contact and job it was for, so draft.sent can name
-- them even when it is emitted later by the reconciler.

ALTER TABLE sends ADD COLUMN contact_id uuid;
ALTER TABLE sends ADD COLUMN job_id uuid;
CREATE INDEX sends_open ON sends (created_at) WHERE status = 'sending';
-- A draft version has at most one send that is in flight or done, even if two
-- valid tokens were ever issued for it. A failed send frees the version.
CREATE UNIQUE INDEX sends_one_live ON sends (draft_id, draft_version) WHERE status <> 'failed';

-- +goose Down
DROP INDEX sends_one_live;
DROP INDEX sends_open;
ALTER TABLE sends DROP COLUMN job_id;
ALTER TABLE sends DROP COLUMN contact_id;
