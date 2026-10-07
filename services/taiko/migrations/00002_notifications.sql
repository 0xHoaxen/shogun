-- +goose Up
CREATE TABLE notifications (
    id              uuid PRIMARY KEY,
    owner_id        uuid NOT NULL,
    type            text NOT NULL CHECK (type IN (
        'draft_ready', 'draft_failed', 'draft_send_failed', 'interview_invite', 'offer',
        'rejection', 'reply_detected', 'follow_up_due', 'budget_threshold',
        'budget_exhausted', 'daily_digest'
    )),
    title           text NOT NULL,
    body            text,
    link            text,
    source_event_id uuid UNIQUE,
    read_at         timestamptz,
    created_at      timestamptz NOT NULL DEFAULT now()
);

CREATE INDEX notifications_unread ON notifications (owner_id, created_at DESC) WHERE read_at IS NULL;
CREATE INDEX notifications_owner_created ON notifications (owner_id, created_at DESC, id DESC);

CREATE TABLE channel_settings (
    owner_id   uuid NOT NULL,
    channel    text NOT NULL CHECK (channel IN ('in_app', 'email_digest')),
    enabled    boolean NOT NULL DEFAULT true,
    quiet_from time,
    quiet_to   time,
    PRIMARY KEY (owner_id, channel)
);

-- +goose Down
DROP TABLE channel_settings;
DROP TABLE notifications;
