-- +goose Up
ALTER TABLE notifications DROP CONSTRAINT notifications_type_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_type_check CHECK (type IN (
    'draft_ready', 'draft_failed', 'draft_send_failed', 'interview_invite', 'offer',
    'rejection', 'reply_detected', 'follow_up_due', 'budget_threshold',
    'budget_exhausted', 'daily_digest', 'profile_suggestion', 'discovery_match'
));

-- +goose Down
DELETE FROM notifications WHERE type = 'discovery_match';
ALTER TABLE notifications DROP CONSTRAINT notifications_type_check;
ALTER TABLE notifications ADD CONSTRAINT notifications_type_check CHECK (type IN (
    'draft_ready', 'draft_failed', 'draft_send_failed', 'interview_invite', 'offer',
    'rejection', 'reply_detected', 'follow_up_due', 'budget_threshold',
    'budget_exhausted', 'daily_digest', 'profile_suggestion'
));
