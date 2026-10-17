package app_test

import (
	"context"
	"errors"
	"testing"

	"github.com/0xHoaxen/shogun/services/torii/internal/app"
	"github.com/0xHoaxen/shogun/services/torii/internal/app/apptest"
)

func TestSetTimezoneIsSeenOnTheNextRequest(t *testing.T) {
	// Arrange
	ctx := context.Background()
	auth := newAuth(t, newFakeSessions(), apptest.NewMemUsers(), authNow)
	token, session, err := auth.StartSession(ctx, ownerClaims())
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}

	// Act
	user, err := auth.SetTimezone(ctx, session.OwnerID, " Europe/Berlin ")
	after, _, authErr := auth.Authenticate(ctx, token)

	// Assert
	if err != nil || user.Timezone != "Europe/Berlin" {
		t.Fatalf("user %+v, err %v", user, err)
	}
	if authErr != nil || after.Timezone != "Europe/Berlin" {
		t.Fatalf("session timezone %q, err %v", after.Timezone, authErr)
	}
}

func TestSetTimezoneRefusesNamesThatAreNotIANAZones(t *testing.T) {
	for _, name := range []string{"", "  ", "Local", "Mars/Olympus", "not a zone", "../etc/passwd"} {
		t.Run(name, func(t *testing.T) {
			ctx := context.Background()
			auth := newAuth(t, newFakeSessions(), apptest.NewMemUsers(), authNow)
			_, session, _ := auth.StartSession(ctx, ownerClaims())

			_, err := auth.SetTimezone(ctx, session.OwnerID, name)

			if !errors.Is(err, app.ErrInvalidTimezone) {
				t.Fatalf("err = %v, want ErrInvalidTimezone", err)
			}
		})
	}
}

func TestEndAllSessionsSignsTheUserOutEverywhereAndNobodyElse(t *testing.T) {
	// Arrange
	ctx := context.Background()
	auth := newAuth(t, newFakeSessions(), apptest.NewMemUsers(), authNow)
	laptop, session, _ := auth.StartSession(ctx, ownerClaims())
	phone, _, _ := auth.StartSession(ctx, ownerClaims())
	other, _, _ := auth.StartSession(ctx, app.Claims{Subject: "sub-2", Email: "other@example.com", EmailVerified: true})

	// Act
	n, err := auth.EndAllSessions(ctx, session.OwnerID)

	// Assert
	if err != nil || n != 2 {
		t.Fatalf("ended %d, err %v; want 2", n, err)
	}
	for name, token := range map[string]string{"laptop": laptop, "phone": phone} {
		if _, _, err := auth.Authenticate(ctx, token); !errors.Is(err, app.ErrUnauthenticated) {
			t.Errorf("%s still signed in: %v", name, err)
		}
	}
	if _, _, err := auth.Authenticate(ctx, other); err != nil {
		t.Errorf("another user was signed out: %v", err)
	}
}
