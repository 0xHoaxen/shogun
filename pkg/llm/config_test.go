package llm_test

import (
	"testing"

	"github.com/0xHoaxen/shogun/pkg/llm"
)

func TestNewConfigListsOnlyTheServicesFeatures(t *testing.T) {
	tests := []struct {
		service  string
		features []string
	}{
		{"fude", []string{"fude.cover_letter", "fude.outreach", "fude.post"}},
		{"tsubame", []string{"tsubame.classify"}},
		{"katana", []string{"katana.suggest"}},
		{"shinobi", []string{"shinobi.score"}},
	}
	for _, tt := range tests {
		t.Run(tt.service, func(t *testing.T) {
			cfg, err := llm.NewConfig(tt.service, noEnv)
			if err != nil {
				t.Fatalf("NewConfig: %v", err)
			}
			if len(cfg.Features) != len(tt.features) {
				t.Fatalf("got %d features, want %v", len(cfg.Features), tt.features)
			}
			for _, f := range tt.features {
				if fc, ok := cfg.Features[f]; !ok || fc.Model == "" || fc.MaxTokens <= 0 {
					t.Errorf("feature %s = %+v, want a model and a cap", f, fc)
				}
			}
		})
	}
}

func TestNewConfigUsesTheSmallerModelForClassifyAndScore(t *testing.T) {
	tsubame, _ := llm.NewConfig("tsubame", noEnv)
	shinobi, _ := llm.NewConfig("shinobi", noEnv)
	fude, _ := llm.NewConfig("fude", noEnv)

	if got := tsubame.Features["tsubame.classify"].Model; got != "claude-haiku-4-5" {
		t.Errorf("classify model = %s, want haiku", got)
	}
	if got := shinobi.Features["shinobi.score"].Model; got != "claude-haiku-4-5" {
		t.Errorf("score model = %s, want haiku", got)
	}
	if got := fude.Features["fude.post"].Model; got != "claude-opus-5-5" {
		t.Errorf("post model = %s, want opus", got)
	}
}

func TestNewConfigReadsAModelOverrideFromTheEnvironment(t *testing.T) {
	env := map[string]string{"LLM_MODEL_FUDE_COVER_LETTER": "claude-sonnet-5-5"}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }

	cfg, err := llm.NewConfig("fude", lookup)
	if err != nil {
		t.Fatalf("NewConfig: %v", err)
	}

	if got := cfg.Features["fude.cover_letter"].Model; got != "claude-sonnet-5-5" {
		t.Errorf("cover letter model = %s, want the override", got)
	}
	if got := cfg.Features["fude.post"].Model; got != "claude-opus-5-5" {
		t.Errorf("post model = %s, want the default", got)
	}
}

func TestNewConfigRejectsAServiceWithoutFeatures(t *testing.T) {
	for _, service := range []string{"", "kagami"} {
		if _, err := llm.NewConfig(service, noEnv); err == nil {
			t.Errorf("service %q: want an error", service)
		}
	}
}
