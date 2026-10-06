package main

import (
	"strings"
	"testing"
)

func TestNewGmailUsesTheAuthURLOverride(t *testing.T) {
	// Arrange
	env := map[string]string{
		gmailClientIDEnv: "id", gmailClientSecretEnv: "secret", gmailRedirectURLEnv: "http://localhost/cb",
		gmailAuthURLEnv: "https://mock-gmail.test/authorize",
	}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }

	// Act
	client, err := newGmail(lookup)
	// Assert
	if err != nil {
		t.Fatalf("newGmail: %v", err)
	}
	if got := client.AuthURL("state", "verifier"); !strings.HasPrefix(got, "https://mock-gmail.test/authorize?") {
		t.Fatalf("AuthURL = %q, want the override host", got)
	}
}
