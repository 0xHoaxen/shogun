package config

import (
	"testing"
	"time"
)

func lookupFrom(m map[string]string) LookupFunc {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func TestLoadBase(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
		check   func(t *testing.T, b Base)
	}{
		{
			name: "defaults in local",
			env:  map[string]string{},
			check: func(t *testing.T, b Base) {
				if b.Environment != EnvLocal || b.LogLevel != "info" || b.GRPCPort != 9090 ||
					b.HTTPPort != 8080 || b.ShutdownTimeout != 15*time.Second || b.Service != "kagami" {
					t.Fatalf("unexpected defaults: %+v", b)
				}
			},
		},
		{
			name: "overrides",
			env: map[string]string{
				"ENVIRONMENT": "production", "DATABASE_URL": "postgres://x", "GRPC_PORT": "7000",
				"HTTP_PORT": "7001", "SHUTDOWN_TIMEOUT": "3s", "LOG_LEVEL": "debug",
			},
			check: func(t *testing.T, b Base) {
				if !b.IsProduction() || b.GRPCPort != 7000 || b.HTTPPort != 7001 ||
					b.ShutdownTimeout != 3*time.Second || b.LogLevel != "debug" {
					t.Fatalf("unexpected config: %+v", b)
				}
			},
		},
		{name: "missing database url outside local", env: map[string]string{"ENVIRONMENT": "staging"}, wantErr: true},
		{name: "database url optional in local", env: map[string]string{"ENVIRONMENT": "local"}},
		{name: "unknown environment", env: map[string]string{"ENVIRONMENT": "prod", "DATABASE_URL": "x"}, wantErr: true},
		{name: "port not a number", env: map[string]string{"GRPC_PORT": "abc"}, wantErr: true},
		{name: "port out of range", env: map[string]string{"HTTP_PORT": "70000"}, wantErr: true},
		{name: "bad duration", env: map[string]string{"SHUTDOWN_TIMEOUT": "soon"}, wantErr: true},
		{name: "non-positive duration", env: map[string]string{"SHUTDOWN_TIMEOUT": "0s"}, wantErr: true},
		{name: "empty values use defaults", env: map[string]string{"GRPC_PORT": "", "SHUTDOWN_TIMEOUT": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := LoadBase("kagami", lookupFrom(tt.env))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && tt.check != nil {
				tt.check(t, got)
			}
		})
	}
}

func TestLoadBaseRequiresService(t *testing.T) {
	if _, err := LoadBase("", lookupFrom(nil)); err == nil {
		t.Fatal("expected error for empty service")
	}
}

func TestRequired(t *testing.T) {
	if _, err := Required(lookupFrom(nil), "KEY"); err == nil {
		t.Fatal("expected error for unset key")
	}
	v, err := Required(lookupFrom(map[string]string{"KEY": "v"}), "KEY")
	if err != nil || v != "v" {
		t.Fatalf("got %q, %v", v, err)
	}
}
