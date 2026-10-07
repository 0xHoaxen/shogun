package domain

import (
	"errors"
	"time"
)

// ErrInvalidQuietHours means a quiet window is not usable.
var ErrInvalidQuietHours = errors.New("domain: quiet hours must be two different whole minutes within a day")

// Validate checks that both ends are whole minutes within a day and differ.
// (An equal pair would mean no window, which is expressed by having none.)
func (q QuietHours) Validate() error {
	for _, end := range []time.Duration{q.From, q.To} {
		if end < 0 || end >= day || end%time.Minute != 0 {
			return ErrInvalidQuietHours
		}
	}
	if q.From == q.To {
		return ErrInvalidQuietHours
	}
	return nil
}

// ChannelSettings is how the owner is reached on the in-app channel.
type ChannelSettings struct {
	InAppEnabled bool
	// Quiet is nil when there are no quiet hours.
	Quiet *QuietHours
}

// DefaultChannelSettings applies until the owner saves their own.
func DefaultChannelSettings() ChannelSettings {
	return ChannelSettings{InAppEnabled: true}
}

// Validate checks the quiet window, when there is one.
func (c ChannelSettings) Validate() error {
	if c.Quiet == nil {
		return nil
	}
	return c.Quiet.Validate()
}
