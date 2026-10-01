// Package config loads service configuration from environment variables.
package config

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

// Environment names accepted in ENVIRONMENT.
const (
	EnvLocal      = "local"
	EnvTest       = "test"
	EnvStaging    = "staging"
	EnvProduction = "production"
)

const (
	defaultLogLevel        = "info"
	defaultGRPCPort        = 9090
	defaultHTTPPort        = 8080
	defaultShutdownTimeout = 15 * time.Second
	maxPort                = 65535
)

// LookupFunc reads one variable, like os.LookupEnv.
type LookupFunc func(key string) (string, bool)

// Base is the configuration every service shares.
type Base struct {
	Service         string
	Environment     string
	LogLevel        string
	DatabaseURL     string
	GRPCPort        int
	HTTPPort        int
	ShutdownTimeout time.Duration
	OTLPEndpoint    string
}

// IsProduction reports whether the service runs in production.
func (b Base) IsProduction() bool { return b.Environment == EnvProduction }

// LoadBase reads and validates the shared configuration for service.
func LoadBase(service string, lookup LookupFunc) (Base, error) {
	if service == "" {
		return Base{}, errors.New("config: service name is required")
	}
	var errs []error
	collectInt := func(v int, err error) int {
		if err != nil {
			errs = append(errs, err)
		}
		return v
	}

	cfg := Base{
		Service:      service,
		Environment:  String(lookup, "ENVIRONMENT", EnvLocal),
		LogLevel:     String(lookup, "LOG_LEVEL", defaultLogLevel),
		DatabaseURL:  String(lookup, "DATABASE_URL", ""),
		OTLPEndpoint: String(lookup, "OTEL_EXPORTER_OTLP_ENDPOINT", ""),
	}
	cfg.GRPCPort = collectInt(Port(lookup, "GRPC_PORT", defaultGRPCPort))
	cfg.HTTPPort = collectInt(Port(lookup, "HTTP_PORT", defaultHTTPPort))
	timeout, err := Duration(lookup, "SHUTDOWN_TIMEOUT", defaultShutdownTimeout)
	if err != nil {
		errs = append(errs, err)
	}
	cfg.ShutdownTimeout = timeout

	switch cfg.Environment {
	case EnvLocal, EnvTest, EnvStaging, EnvProduction:
	default:
		errs = append(errs, fmt.Errorf("config: ENVIRONMENT %q must be one of local, test, staging, production", cfg.Environment))
	}
	if cfg.Environment != EnvLocal && cfg.DatabaseURL == "" {
		errs = append(errs, errors.New("config: DATABASE_URL is required outside local"))
	}
	if err := errors.Join(errs...); err != nil {
		return Base{}, err
	}
	return cfg, nil
}

// String returns the variable's value, or def when it is unset or empty.
func String(lookup LookupFunc, key, def string) string {
	if v, ok := lookup(key); ok && v != "" {
		return v
	}
	return def
}

// Required returns the variable's value or an error when it is unset or empty.
func Required(lookup LookupFunc, key string) (string, error) {
	v, ok := lookup(key)
	if !ok || v == "" {
		return "", fmt.Errorf("config: %s is required", key)
	}
	return v, nil
}

// Int parses an integer variable, falling back to def when unset or empty.
func Int(lookup LookupFunc, key string, def int) (int, error) {
	v, ok := lookup(key)
	if !ok || v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def, fmt.Errorf("config: %s: %w", key, err)
	}
	return n, nil
}

// Port parses a TCP port variable in 1..65535.
func Port(lookup LookupFunc, key string, def int) (int, error) {
	n, err := Int(lookup, key, def)
	if err != nil {
		return def, err
	}
	if n < 1 || n > maxPort {
		return def, fmt.Errorf("config: %s %d is out of range 1..%d", key, n, maxPort)
	}
	return n, nil
}

// Duration parses a time.Duration variable, falling back to def when unset or empty.
func Duration(lookup LookupFunc, key string, def time.Duration) (time.Duration, error) {
	v, ok := lookup(key)
	if !ok || v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return def, fmt.Errorf("config: %s: %w", key, err)
	}
	if d <= 0 {
		return def, fmt.Errorf("config: %s must be positive, got %s", key, v)
	}
	return d, nil
}
