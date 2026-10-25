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
		{"fude", []string{"fude.cover_letter", "fude.outreach", "fude.post", "fude.test_connection"}},
		{"tsubame", []string{"tsubame.classify"}},
		{"katana", []string{"katana.suggest", "katana.resume_parse", "katana.job_skills"}},
		{"shinobi", []string{"shinobi.score"}},
	}
	for _, tt := range tests {
		t.Run(tt.service, func(t *testing.T) {
			cfg, err := llm.NewConfig(tt.service)
			if err != nil {
				t.Fatalf("NewConfig: %v", err)
			}
			if len(cfg.Features) != len(tt.features) {
				t.Fatalf("got %d features, want %v", len(cfg.Features), tt.features)
			}
			for _, f := range tt.features {
				if fc, ok := cfg.Features[f]; !ok || fc.MaxTokens <= 0 {
					t.Errorf("feature %s = %+v, want an output cap", f, fc)
				}
			}
		})
	}
}

func TestNewConfigRejectsAServiceWithoutFeatures(t *testing.T) {
	for _, service := range []string{"", "dojo"} {
		if _, err := llm.NewConfig(service); err == nil {
			t.Errorf("service %q: want an error", service)
		}
	}
}
