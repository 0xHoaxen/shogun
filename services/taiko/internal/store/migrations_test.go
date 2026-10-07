package store_test

import (
	"context"
	"testing"

	"github.com/0xHoaxen/shogun/pkg/postgres"
	"github.com/0xHoaxen/shogun/pkg/postgres/postgrestest"
	"github.com/0xHoaxen/shogun/services/taiko/migrations"
)

func TestMigrationsEnforceTheNotificationChecks(t *testing.T) {
	ctx := context.Background()
	pool, err := postgres.Connect(ctx, postgrestest.NewDatabase(t), "taiko")
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	tests := []struct {
		name    string
		query   string
		wantErr bool
	}{
		{"known type", `INSERT INTO notifications (id, owner_id, type, title) VALUES (gen_random_uuid(), gen_random_uuid(), 'offer', 'x')`, false},
		{"profile suggestion type", `INSERT INTO notifications (id, owner_id, type, title) VALUES (gen_random_uuid(), gen_random_uuid(), 'profile_suggestion', 'x')`, false},
		{"discovery match type", `INSERT INTO notifications (id, owner_id, type, title) VALUES (gen_random_uuid(), gen_random_uuid(), 'discovery_match', 'x')`, false},
		{"unknown type", `INSERT INTO notifications (id, owner_id, type, title) VALUES (gen_random_uuid(), gen_random_uuid(), 'bogus', 'x')`, true},
		{"known channel", `INSERT INTO channel_settings (owner_id, channel) VALUES (gen_random_uuid(), 'in_app')`, false},
		{"unknown channel", `INSERT INTO channel_settings (owner_id, channel) VALUES (gen_random_uuid(), 'sms')`, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, tt.query)

			if (err != nil) != tt.wantErr {
				t.Fatalf("wantErr %v, got %v", tt.wantErr, err)
			}
		})
	}
}
