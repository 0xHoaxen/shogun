package app

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/0xHoaxen/shogun/services/taiko/internal/domain"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store/db"
)

// ReasonInvalidChannelSettings is the reason for settings that fail validation.
const ReasonInvalidChannelSettings = "CHANNEL_SETTINGS_INVALID"

// SettingsView is the owner's in-app settings and the version to save them at.
// Version is zero until they are first saved.
type SettingsView struct {
	domain.ChannelSettings
	Version int64
}

// ChannelSettings returns the owner's in-app settings, or the defaults when
// they never saved any.
func (s *Service) ChannelSettings(ctx context.Context) (SettingsView, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return SettingsView{}, err
	}
	row, err := store.New(s.pool).ChannelSetting(ctx, owner, store.ChannelInApp)
	if errors.Is(err, store.ErrNotFound) {
		return SettingsView{ChannelSettings: domain.DefaultChannelSettings()}, nil
	}
	if err != nil {
		return SettingsView{}, err
	}
	return viewOf(row), nil
}

// SaveChannelSettings replaces the owner's in-app settings. version is the one
// last read; a stale one returns store.ErrVersionConflict.
func (s *Service) SaveChannelSettings(ctx context.Context, in domain.ChannelSettings, version int64) (SettingsView, error) {
	owner, err := ownerFrom(ctx)
	if err != nil {
		return SettingsView{}, err
	}
	if version < 0 {
		return SettingsView{}, &InvalidArgumentError{Reason: ReasonInvalidChannelSettings, Msg: "version must not be negative"}
	}
	if err := in.Validate(); err != nil {
		return SettingsView{}, &InvalidArgumentError{Reason: ReasonInvalidChannelSettings, Msg: err.Error()}
	}
	row, err := store.New(s.pool).SaveChannelSetting(ctx, store.ChannelSettingInput{
		OwnerID: owner, Channel: store.ChannelInApp, Enabled: in.InAppEnabled,
		QuietFrom: timeColumn(in.Quiet, func(q domain.QuietHours) time.Duration { return q.From }),
		QuietTo:   timeColumn(in.Quiet, func(q domain.QuietHours) time.Duration { return q.To }),
		Version:   version,
	})
	if err != nil {
		return SettingsView{}, err
	}
	return viewOf(row), nil
}

func timeColumn(q *domain.QuietHours, end func(domain.QuietHours) time.Duration) pgtype.Time {
	if q == nil {
		return pgtype.Time{}
	}
	return pgtype.Time{Microseconds: end(*q).Microseconds(), Valid: true}
}

func viewOf(row db.ChannelSetting) SettingsView {
	view := SettingsView{
		ChannelSettings: domain.ChannelSettings{InAppEnabled: row.Enabled},
		Version:         row.Version,
	}
	if row.QuietFrom.Valid && row.QuietTo.Valid {
		view.Quiet = &domain.QuietHours{
			From: time.Duration(row.QuietFrom.Microseconds) * time.Microsecond,
			To:   time.Duration(row.QuietTo.Microseconds) * time.Microsecond,
		}
	}
	return view
}
