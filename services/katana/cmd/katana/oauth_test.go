package main

import (
	"strings"
	"testing"

	"github.com/0xHoaxen/shogun/pkg/config"
)

func lookupOf(env map[string]string) config.LookupFunc {
	return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
}

func TestGitHubOAuthSettings(t *testing.T) {
	all := map[string]string{
		githubClientIDEnv: "Iv1.abc", githubClientSecretEnv: "s3cret-value", githubRedirectURLEnv: "https://shogun.example/settings/github/callback",
	}
	without := func(key string) map[string]string {
		out := map[string]string{}
		for k, v := range all {
			if k != key {
				out[k] = v
			}
		}
		return out
	}
	tests := []struct {
		name       string
		env        map[string]string
		production bool
		wantOn     bool
		wantErr    string
	}{
		{"all three set", all, false, true, ""},
		{"none set outside production leaves connecting off", map[string]string{}, false, false, ""},
		{"none set in production is refused", map[string]string{}, true, false, githubClientIDEnv},
		{"a missing secret is refused", without(githubClientSecretEnv), false, false, githubClientSecretEnv},
		{"a missing id is refused", without(githubClientIDEnv), false, false, githubClientIDEnv},
		{"a missing redirect is refused", without(githubRedirectURLEnv), true, false, githubRedirectURLEnv},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oauth, err := newGitHubOAuth(lookupOf(tt.env), tt.production)

			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("err = %v, want it to name %s", err, tt.wantErr)
				}
				return
			}
			if err != nil || (oauth != nil) != tt.wantOn {
				t.Fatalf("oauth %v, err %v, want on=%v", oauth, err, tt.wantOn)
			}
		})
	}
}
