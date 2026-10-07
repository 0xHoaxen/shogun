package app_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/0xHoaxen/shogun/services/taiko/internal/app"
	"github.com/0xHoaxen/shogun/services/taiko/internal/domain"
	"github.com/0xHoaxen/shogun/services/taiko/internal/store"
)

func TestChannelSettingsDefaultUntilSaved(t *testing.T) {
	svc, _ := newService(t)

	got, err := svc.ChannelSettings(asOwner(uuid.New()))

	if err != nil || !got.InAppEnabled || got.Quiet != nil || got.Version != 0 {
		t.Fatalf("got %+v, %v; want in-app on, no quiet hours, version 0", got, err)
	}
}

func TestSaveChannelSettingsRoundTripsAndBumpsTheVersion(t *testing.T) {
	svc, _ := newService(t)
	ctx := asOwner(uuid.New())
	quiet := &domain.QuietHours{From: 22 * time.Hour, To: 8 * time.Hour}

	first, err1 := svc.SaveChannelSettings(ctx, domain.ChannelSettings{InAppEnabled: true, Quiet: quiet}, 0)
	second, err2 := svc.SaveChannelSettings(ctx, domain.ChannelSettings{InAppEnabled: false}, first.Version)
	read, err3 := svc.ChannelSettings(ctx)

	if err1 != nil || err2 != nil || err3 != nil {
		t.Fatalf("errors %v, %v, %v", err1, err2, err3)
	}
	if first.Quiet == nil || *first.Quiet != *quiet || first.Version != 1 {
		t.Fatalf("first save read back as %+v", first)
	}
	if read.InAppEnabled || read.Quiet != nil || read.Version != 2 || second.Version != 2 {
		t.Fatalf("second save read back as %+v", read)
	}
}

func TestSaveChannelSettingsRefusals(t *testing.T) {
	svc, _ := newService(t)
	ctx := asOwner(uuid.New())
	if _, err := svc.SaveChannelSettings(ctx, domain.DefaultChannelSettings(), 0); err != nil {
		t.Fatalf("seed: %v", err)
	}
	var invalid *app.InvalidArgumentError

	_, errBad := svc.SaveChannelSettings(ctx, domain.ChannelSettings{Quiet: &domain.QuietHours{From: time.Hour, To: time.Hour}}, 1)
	_, errNegative := svc.SaveChannelSettings(ctx, domain.DefaultChannelSettings(), -1)
	_, errStale := svc.SaveChannelSettings(ctx, domain.DefaultChannelSettings(), 9)
	_, errNoOwner := svc.SaveChannelSettings(context.Background(), domain.DefaultChannelSettings(), 0)
	_, errGetNoOwner := svc.ChannelSettings(context.Background())

	if !errors.As(errBad, &invalid) || invalid.Reason != app.ReasonInvalidChannelSettings {
		t.Fatalf("bad window: %v", errBad)
	}
	if !errors.As(errNegative, &invalid) {
		t.Fatalf("negative version: %v", errNegative)
	}
	if !errors.Is(errStale, store.ErrVersionConflict) {
		t.Fatalf("stale: %v", errStale)
	}
	if !errors.Is(errNoOwner, app.ErrNoOwner) || !errors.Is(errGetNoOwner, app.ErrNoOwner) {
		t.Fatalf("no owner: %v, %v", errNoOwner, errGetNoOwner)
	}
}

// A quiet window saved through the use case, not written to the table by the
// test, holds the next digest back, and the digest runs once the window ends.
func TestASavedQuietWindowHoldsTheNextDigestBack(t *testing.T) {
	e := newDigestEnv(t, atDigestTime)
	owner := e.owner(t)
	e.sources.followUps = 1
	svc := newServiceOn(t, e.pool)
	window := domain.ChannelSettings{InAppEnabled: true, Quiet: &domain.QuietHours{From: hours(8, 0), To: hours(9, 0)}}

	saved, err := svc.SaveChannelSettings(asOwner(owner), window, 0)
	wait, runErr := e.digester.Run(context.Background(), digestDay)
	heldDigests := len(e.digests(t, owner))
	_, clearErr := svc.SaveChannelSettings(asOwner(owner), domain.DefaultChannelSettings(), saved.Version)
	rerun, rerunErr := e.digester.Run(context.Background(), digestDay)

	if err != nil || runErr != nil || clearErr != nil || rerunErr != nil {
		t.Fatalf("errors %v, %v, %v, %v", err, runErr, clearErr, rerunErr)
	}
	if wait != 30*time.Minute || heldDigests != 0 {
		t.Fatalf("wait %s with %d digests; want held back for 30m with none written", wait, heldDigests)
	}
	if rerun != 0 || len(e.digests(t, owner)) != 1 {
		t.Fatalf("after clearing the window: wait %s, %d digests; want one written", rerun, len(e.digests(t, owner)))
	}
}
