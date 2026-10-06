package store

import (
	"context"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/taiko/internal/store/db"
)

// Channels a notification can be delivered on.
const (
	ChannelInApp       = "in_app"
	ChannelEmailDigest = "email_digest"
)

// ChannelSetting returns the owner's setting for a channel, or ErrNotFound when
// they never changed it (the defaults then apply).
func (r *Repo) ChannelSetting(ctx context.Context, owner uuid.UUID, channel string) (db.ChannelSetting, error) {
	row, err := r.q.GetChannelSetting(ctx, db.GetChannelSettingParams{OwnerID: owner, Channel: channel})
	return row, wrap("get channel setting", err)
}

// SaveChannelSetting creates or replaces the owner's setting for a channel.
func (r *Repo) SaveChannelSetting(ctx context.Context, p db.UpsertChannelSettingParams) (db.ChannelSetting, error) {
	row, err := r.q.UpsertChannelSetting(ctx, p)
	return row, wrap("save channel setting", err)
}
