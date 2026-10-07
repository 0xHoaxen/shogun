package store

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

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

// ChannelSettingInput is a setting to write. Version is the version the caller
// read, or zero when it read none because the setting did not exist.
type ChannelSettingInput struct {
	OwnerID   uuid.UUID
	Channel   string
	Enabled   bool
	QuietFrom pgtype.Time
	QuietTo   pgtype.Time
	Version   int64
}

// SaveChannelSetting creates the owner's setting for a channel (version zero)
// or replaces it when the stored version is still in.Version. Otherwise it
// returns ErrVersionConflict.
func (r *Repo) SaveChannelSetting(ctx context.Context, in ChannelSettingInput) (db.ChannelSetting, error) {
	var row db.ChannelSetting
	var err error
	if in.Version == 0 {
		row, err = r.q.InsertChannelSetting(ctx, db.InsertChannelSettingParams{
			OwnerID: in.OwnerID, Channel: in.Channel, Enabled: in.Enabled,
			QuietFrom: in.QuietFrom, QuietTo: in.QuietTo,
		})
	} else {
		row, err = r.q.UpdateChannelSetting(ctx, db.UpdateChannelSettingParams{
			OwnerID: in.OwnerID, Channel: in.Channel, Enabled: in.Enabled,
			QuietFrom: in.QuietFrom, QuietTo: in.QuietTo, Version: in.Version,
		})
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return db.ChannelSetting{}, ErrVersionConflict
	}
	return row, wrap("save channel setting", err)
}

// Owners returns every owner taiko holds a notification or a setting for.
func (r *Repo) Owners(ctx context.Context) ([]uuid.UUID, error) {
	owners, err := r.q.ListOwners(ctx)
	return owners, wrap("list owners", err)
}
