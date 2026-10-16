package domain_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/0xHoaxen/shogun/services/soroban/internal/domain"
)

func TestNormalizeBaseURL(t *testing.T) {
	tests := []struct {
		name          string
		in            string
		allowInsecure bool
		want          string
		wantErr       bool
	}{
		{"anthropic", "https://api.anthropic.com", false, "https://api.anthropic.com", false},
		{"surrounding space and a trailing slash go", "  https://api.example.com/v1/  ", false, "https://api.example.com/v1", false},
		{"a port is fine", "https://llm.example.com:8443/v1", false, "https://llm.example.com:8443/v1", false},
		{"the host is lower-cased", "https://API.Example.COM", false, "https://api.example.com", false},
		{"empty", "", false, "", true},
		{"plain http", "http://api.example.com", false, "", true},
		{"plain http where insecure is allowed", "http://localhost:8080", true, "http://localhost:8080", false},
		{"another scheme", "ftp://api.example.com", false, "", true},
		{"no scheme", "api.example.com", false, "", true},
		{"no host", "https://", false, "", true},
		{"credentials in the address", "https://user:pass@api.example.com", false, "", true},
		{"a query", "https://api.example.com/v1?key=1", false, "", true},
		{"a fragment", "https://api.example.com/#x", false, "", true},
		{"loopback address", "https://127.0.0.1", false, "", true},
		{"private address", "https://10.1.2.3/v1", false, "", true},
		{"cloud metadata address", "https://169.254.169.254", false, "", true},
		{"IPv6 loopback", "https://[::1]:443", false, "", true},
		{"a private address written as IPv6", "https://[::ffff:10.0.0.1]", false, "", true},
		{"localhost", "https://localhost", false, "", true},
		{"a name inside the network", "https://soroban", false, "", true},
		{"a cluster name", "https://soroban.shogun.svc.cluster.local", false, "", true},
		{"an internal suffix", "https://db.internal", false, "", true},
		{"a private address where insecure is allowed", "http://10.1.2.3", true, "http://10.1.2.3", false},
		{"a public IP literal", "https://8.8.8.8/v1", false, "https://8.8.8.8/v1", false},
		{"too long", "https://example.com/" + strings.Repeat("a", 3000), false, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := domain.NormalizeBaseURL(tt.in, tt.allowInsecure)

			if (err != nil) != tt.wantErr || got != tt.want {
				t.Fatalf("NormalizeBaseURL(%q) = %q, %v; want %q, error %v", tt.in, got, err, tt.want, tt.wantErr)
			}
			if err != nil && !errors.Is(err, domain.ErrInvalidProviderURL) {
				t.Fatalf("err = %v, want ErrInvalidProviderURL", err)
			}
		})
	}
}

func TestValidateModel(t *testing.T) {
	good := []string{"claude-sonnet-5-5", "anthropic/claude-opus-5-5", "llama3.1:70b", "gpt_like.v2", "m"}
	bad := []string{"", " ", "has space", "new\nline", "tab\t", strings.Repeat("a", 201), "semi;colon", "quote\"d"}
	for _, m := range good {
		if err := domain.ValidateModel(m); err != nil {
			t.Errorf("ValidateModel(%q) = %v, want ok", m, err)
		}
	}
	for _, m := range bad {
		if err := domain.ValidateModel(m); !errors.Is(err, domain.ErrInvalidProviderModel) {
			t.Errorf("ValidateModel(%q) = %v, want ErrInvalidProviderModel", m, err)
		}
	}
}

func TestValidateAPIKey(t *testing.T) {
	good := []string{"sk-ant-api03-abcdefghijklmnop", "12345678", strings.Repeat("k", 4096)}
	bad := []string{"", "short", "has space inside", "new\nline", "tab\tkey", "ünïcode-key-1234", strings.Repeat("k", 4097)}
	for _, k := range good {
		if err := domain.ValidateAPIKey(k); err != nil {
			t.Errorf("ValidateAPIKey(%d chars) = %v, want ok", len(k), err)
		}
	}
	for _, k := range bad {
		err := domain.ValidateAPIKey(k)
		if !errors.Is(err, domain.ErrInvalidAPIKey) {
			t.Errorf("ValidateAPIKey(%q) = %v, want ErrInvalidAPIKey", k, err)
		}
		if err != nil && k != "" && strings.Contains(err.Error(), k) {
			t.Errorf("the error repeats the key: %v", err)
		}
	}
}

func TestKeyLast4(t *testing.T) {
	if got := domain.KeyLast4("sk-ant-api03-abcdWXYZ"); got != "WXYZ" {
		t.Fatalf("KeyLast4 = %q, want WXYZ", got)
	}
	if got := domain.KeyLast4("12345678"); got != "5678" {
		t.Fatalf("KeyLast4 = %q, want 5678", got)
	}
}

func TestValidateProviderPrices(t *testing.T) {
	one, zero, neg, huge := int64(3_000_000), int64(0), int64(-1), int64(2_000_000_000_000)
	tests := []struct {
		name    string
		in, out *int64
		wantErr bool
	}{
		{"none", nil, nil, false},
		{"both", &one, &one, false},
		{"free", &zero, &zero, false},
		{"only input", &one, nil, true},
		{"only output", nil, &one, true},
		{"negative", &neg, &one, true},
		{"absurd", &one, &huge, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := domain.ValidateProviderPrices(tt.in, tt.out)

			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, want error %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, domain.ErrInvalidProviderPrice) {
				t.Fatalf("err = %v, want ErrInvalidProviderPrice", err)
			}
		})
	}
}
