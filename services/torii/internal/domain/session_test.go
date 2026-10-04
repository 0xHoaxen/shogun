package domain_test

import (
	"testing"
	"time"

	"github.com/0xHoaxen/shogun/services/torii/internal/domain"
)

var (
	sessionNow = time.Date(2026, 10, 4, 9, 0, 0, 0, time.UTC)
	sessionTTL = 24 * time.Hour
)

func TestSessionExpired(t *testing.T) {
	tests := []struct {
		name      string
		expiresAt time.Time
		want      bool
	}{
		{"valid for another hour", sessionNow.Add(time.Hour), false},
		{"expires exactly now", sessionNow, true},
		{"expired an hour ago", sessionNow.Add(-time.Hour), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			s := domain.Session{ExpiresAt: tt.expiresAt}

			// Act
			got := s.Expired(sessionNow)

			// Assert
			if got != tt.want {
				t.Fatalf("Expired = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSessionRenewed(t *testing.T) {
	tests := []struct {
		name        string
		remaining   time.Duration
		wantRenewed bool
	}{
		{"fresh session is left alone", sessionTTL, false},
		{"just over half left is left alone", sessionTTL/2 + time.Minute, false},
		{"under half left is renewed", sessionTTL/2 - time.Minute, true},
		{"about to expire is renewed", time.Second, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			original := domain.Session{ExpiresAt: sessionNow.Add(tt.remaining)}

			// Act
			got, renewed := original.Renewed(sessionNow, sessionTTL)

			// Assert
			if renewed != tt.wantRenewed {
				t.Fatalf("renewed = %v, want %v", renewed, tt.wantRenewed)
			}
			wantExpiry := original.ExpiresAt
			if tt.wantRenewed {
				wantExpiry = sessionNow.Add(sessionTTL)
			}
			if !got.ExpiresAt.Equal(wantExpiry) {
				t.Fatalf("ExpiresAt = %v, want %v", got.ExpiresAt, wantExpiry)
			}
		})
	}
}

func TestSessionRenewedDoesNotMutateReceiver(t *testing.T) {
	// Arrange
	original := domain.Session{ExpiresAt: sessionNow.Add(time.Minute)}

	// Act
	_, _ = original.Renewed(sessionNow, sessionTTL)

	// Assert
	if !original.ExpiresAt.Equal(sessionNow.Add(time.Minute)) {
		t.Fatalf("receiver mutated: ExpiresAt = %v", original.ExpiresAt)
	}
}
