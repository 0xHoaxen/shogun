package netguard_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/0xHoaxen/shogun/pkg/netguard"
)

func TestIsPublicOnlyAcceptsAddressesOnTheInternet(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"8.8.8.8", true},
		{"2606:4700:4700::1111", true},
		{"127.0.0.1", false},
		{"::1", false},
		{"10.0.0.5", false},
		{"172.16.0.1", false},
		{"192.168.1.1", false},
		{"169.254.169.254", false}, // cloud metadata
		{"fe80::1", false},
		{"fd00::1", false},
		{"100.64.0.1", false}, // carrier-grade NAT
		{"0.0.0.0", false},
		{"::", false},
		{"224.0.0.1", false},
		{"::ffff:10.0.0.1", false}, // a private address written as IPv6
		{"::ffff:127.0.0.1", false},
	}
	for _, tt := range tests {
		t.Run(tt.addr, func(t *testing.T) {
			if got := netguard.IsPublic(netip.MustParseAddr(tt.addr)); got != tt.want {
				t.Fatalf("IsPublic(%s) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
	if netguard.IsPublic(netip.Addr{}) {
		t.Fatal("the zero address counted as public")
	}
}

func TestDialControlRefusesWhatItIsAskedToConnectTo(t *testing.T) {
	tests := map[string]bool{
		"8.8.8.8:443":         true,
		"127.0.0.1:8080":      false,
		"[::1]:443":           false,
		"10.1.2.3:443":        false,
		"169.254.169.254:80":  false,
		"not-an-address":      false,
		"example.com:443":     false, // a name here means the caller skipped DNS
		"[2606:4700::1111]:1": true,
	}
	for address, allowed := range tests {
		t.Run(address, func(t *testing.T) {
			err := netguard.DialControl("tcp", address, nil)

			if allowed && err != nil {
				t.Fatalf("refused %s: %v", address, err)
			}
			if !allowed && !errors.Is(err, netguard.ErrNotPublic) {
				t.Fatalf("err = %v, want ErrNotPublic", err)
			}
		})
	}
}

func TestTransportNeverConnectsToThisMachine(t *testing.T) {
	hit := false
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hit = true }))
	defer srv.Close()
	client := &http.Client{Transport: netguard.Transport()}
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)

	resp, err := client.Do(req)
	if err == nil {
		_ = resp.Body.Close()
	}

	if !errors.Is(err, netguard.ErrNotPublic) || hit {
		t.Fatalf("err = %v, server reached = %v; want ErrNotPublic and no request", err, hit)
	}
}

func TestTransportIgnoresProxySettings(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")

	if netguard.Transport().Proxy != nil {
		t.Fatal("the guarded transport honours proxy settings, which would let a request leave by another route")
	}
}
