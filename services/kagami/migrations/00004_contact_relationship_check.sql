-- +goose Up
-- A contact's relationship is one of six values (recruiter, engineer, manager,
-- alumni, friend, other). The app has always checked it, but older rows, and a
-- CSV imported before the check, may hold other words such as "Referral". Tidy
-- the case first, send anything still unknown to "other", then make the database
-- refuse a value outside the six.
UPDATE contacts SET relationship = lower(btrim(relationship))
WHERE relationship IS NOT NULL AND relationship <> lower(btrim(relationship));

-- Blank text means no relationship, which is NULL everywhere else. This has to
-- come before the step below, or a blank would be called "other".
UPDATE contacts SET relationship = NULL WHERE relationship = '';

UPDATE contacts SET relationship = 'other'
WHERE relationship IS NOT NULL
  AND relationship NOT IN ('recruiter', 'engineer', 'manager', 'alumni', 'friend', 'other');

ALTER TABLE contacts ADD CONSTRAINT contacts_relationship_check
    CHECK (relationship IS NULL OR relationship IN ('recruiter', 'engineer', 'manager', 'alumni', 'friend', 'other'));

-- +goose Down
ALTER TABLE contacts DROP CONSTRAINT contacts_relationship_check;
