package app

import (
	"errors"
	"testing"

	"github.com/0xHoaxen/shogun/services/tsubame/internal/domain"
)

func TestParseClassification(t *testing.T) {
	tests := []struct {
		name      string
		text      string
		wantClass domain.Class
		wantConf  float32
		wantErr   bool
	}{
		{"plain", `{"classification":"rejection","confidence":0.93}`, domain.ClassRejection, 0.93, false},
		{"text around it", "Sure:\n```json\n{\"classification\": \"Offer\", \"confidence\": 1}\n```", domain.ClassOffer, 1, false},
		{"confidence above one is capped", `{"classification":"other","confidence":7}`, domain.ClassOther, 1, false},
		{"negative confidence is floored", `{"classification":"other","confidence":-2}`, domain.ClassOther, 0, false},
		{"unknown class", `{"classification":"spam","confidence":0.9}`, "", 0, true},
		{"missing confidence", `{"classification":"offer"}`, "", 0, true},
		{"no json", "it is an offer", "", 0, true},
		{"broken json", `{"classification":`, "", 0, true},
		{"empty", "", "", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			class, conf, err := parseClassification(tt.text)

			if (err != nil) != tt.wantErr || (err != nil && !errors.Is(err, ErrBadModelAnswer)) || class != tt.wantClass || conf != tt.wantConf {
				t.Fatalf("got %q %v %v", class, conf, err)
			}
		})
	}
}

func TestURLsInStripTrailingPunctuation(t *testing.T) {
	got := urlsIn("See https://a.example/x, and (https://b.example/y). Also http://c.example/z?q=1!")

	want := []string{"https://a.example/x", "https://b.example/y", "http://c.example/z?q=1"}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("url %d = %q, want %q", i, got[i], want[i])
		}
	}
}
