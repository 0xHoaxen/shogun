package llm

import (
	"fmt"
	"strings"
	"time"

	"github.com/0xHoaxen/shogun/pkg/config"
)

const (
	// modelOpus is the default model for writing.
	modelOpus = "claude-opus-5-5"
	// modelHaiku is the smaller model for classification and scoring.
	modelHaiku = "claude-haiku-4-5"

	// DefaultCacheTTL is how long a cached response is served.
	DefaultCacheTTL = 24 * time.Hour

	// modelEnvPrefix starts the variable that overrides a feature's model:
	// LLM_MODEL_FUDE_COVER_LETTER for fude.cover_letter.
	modelEnvPrefix = "LLM_MODEL_"
)

// FeatureConfig is how one feature calls the model.
type FeatureConfig struct {
	Model string
	// MaxTokens caps the output, and so the cost reserved for the call.
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

// defaultFeatures lists every feature that calls Claude. Drafting uses the
// strongest model; classification and scoring use the smaller one, as the LLD
// asks.
func defaultFeatures() map[string]FeatureConfig {
	return map[string]FeatureConfig{
		"fude.cover_letter": {Model: modelOpus, MaxTokens: 4096},
		"fude.outreach":     {Model: modelOpus, MaxTokens: 2048},
		"fude.post":         {Model: modelOpus, MaxTokens: 2048},
		"tsubame.classify":  {Model: modelHaiku, MaxTokens: 512},
		"katana.suggest":    {Model: modelOpus, MaxTokens: 4096},
		"shinobi.score":     {Model: modelHaiku, MaxTokens: 1024},
	}
}

// NewConfig returns the Config of service: its features with their default
// settings, each model overridable by LLM_MODEL_<FEATURE>.
func NewConfig(service string, lookup config.LookupFunc) (Config, error) {
	if service == "" {
		return Config{}, fmt.Errorf("llm: service is required")
	}
	features := map[string]FeatureConfig{}
	for name, fc := range defaultFeatures() {
		if !strings.HasPrefix(name, service+".") {
			continue
		}
		fc.Model = config.String(lookup, modelEnvPrefix+envName(name), fc.Model)
		features[name] = fc
	}
	if len(features) == 0 {
		return Config{}, fmt.Errorf("llm: service %q has no features", service)
	}
	return Config{Service: service, Features: features, CacheTTL: DefaultCacheTTL}, nil
}

// envName turns fude.cover_letter into FUDE_COVER_LETTER.
func envName(feature string) string {
	return strings.ToUpper(strings.ReplaceAll(feature, ".", "_"))
}
