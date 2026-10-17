package llm

import (
	"fmt"
	"strings"
	"time"
)

// DefaultCacheTTL is how long a cached response is served.
const DefaultCacheTTL = 24 * time.Hour

// FeatureConfig is the settings of one feature that calls the model. The model
// itself is not here: it is the one in the calling user's own provider.
type FeatureConfig struct {
	// MaxTokens caps the output, and the cost reserved for the call.
	MaxTokens int32
}

// Config is what a service's Client runs with.
type Config struct {
	// Service is the calling service, as soroban knows it, for example fude.
	Service string
	// Features maps feature names such as fude.cover_letter to their settings.
	// To add a feature, add it to defaultFeatures and give it a budget row in
	// soroban's seed migration.
	Features map[string]FeatureConfig
	// CacheTTL is how long a cached response is served; zero means
	// DefaultCacheTTL.
	CacheTTL time.Duration
}

// defaultFeatures lists every feature that calls the model, with its output cap.
func defaultFeatures() map[string]FeatureConfig {
	return map[string]FeatureConfig{
		"fude.cover_letter":    {MaxTokens: 4096},
		"fude.outreach":        {MaxTokens: 2048},
		"fude.post":            {MaxTokens: 2048},
		"fude.test_connection": {MaxTokens: 16},
		"tsubame.classify":     {MaxTokens: 512},
		"katana.suggest":       {MaxTokens: 4096},
		"shinobi.score":        {MaxTokens: 1024},
	}
}

// NewConfig returns the Config of service: its features with their settings.
func NewConfig(service string) (Config, error) {
	if service == "" {
		return Config{}, fmt.Errorf("llm: service is required")
	}
	features := map[string]FeatureConfig{}
	for name, fc := range defaultFeatures() {
		if strings.HasPrefix(name, service+".") {
			features[name] = fc
		}
	}
	if len(features) == 0 {
		return Config{}, fmt.Errorf("llm: service %q has no features", service)
	}
	return Config{Service: service, Features: features, CacheTTL: DefaultCacheTTL}, nil
}
