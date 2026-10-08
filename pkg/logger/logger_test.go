package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func decode(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(buf.Bytes(), &m); err != nil {
		t.Fatalf("output is not JSON: %v: %q", err, buf.String())
	}
	return m
}

func TestNewStampsServiceAndVersion(t *testing.T) {
	var buf bytes.Buffer
	New(&buf, "kagami", "info").Info("hello")

	m := decode(t, &buf)
	if m["service"] != "kagami" || m["version"] != "dev" || m["msg"] != "hello" {
		t.Fatalf("unexpected record: %v", m)
	}
}

func TestContextAttrsAreIncluded(t *testing.T) {
	var buf bytes.Buffer
	ctx := WithAttrs(context.Background(), slog.String("request_id", "r1"))
	ctx = WithAttrs(ctx, slog.String("trace_id", "t1"))
	New(&buf, "kagami", "info").InfoContext(ctx, "hello")

	m := decode(t, &buf)
	if m["request_id"] != "r1" || m["trace_id"] != "t1" {
		t.Fatalf("missing context attrs: %v", m)
	}
}

func TestLevelFiltering(t *testing.T) {
	tests := []struct {
		level string
		want  bool
	}{{"debug", true}, {"info", false}, {"bogus", false}}
	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			var buf bytes.Buffer
			New(&buf, "kagami", tt.level).Debug("d")
			if got := buf.Len() > 0; got != tt.want {
				t.Fatalf("debug logged = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSensitiveKeysAreRedactedWhateverTheirCase(t *testing.T) {
	for _, key := range []string{
		"body", "Body", "subject", "snippet", "prompt", "email", "recipient", "recipients",
		"token", "access_token", "refresh_token", "id_token", "api_key", "hanko",
		"password", "secret", "authorization", "Authorization", "cookie", "set-cookie",
	} {
		t.Run(key, func(t *testing.T) {
			// Arrange
			var buf bytes.Buffer

			// Act
			New(&buf, "tsubame", "info").Info("hello", slog.String(key, "do-not-log-this"))

			// Assert
			if got := decode(t, &buf)[key]; got != Redacted {
				t.Errorf("%s = %v, want %q", key, got, Redacted)
			}
			if bytes.Contains(buf.Bytes(), []byte("do-not-log-this")) {
				t.Errorf("the value leaked into the log line: %s", buf.String())
			}
		})
	}
}

func TestSensitiveKeysAreRedactedInsideGroupsAndWith(t *testing.T) {
	// Arrange
	var buf bytes.Buffer
	log := New(&buf, "tsubame", "info").With(slog.String("token", "leak-1"))

	// Act
	log.Info("hello", slog.Group("mail", slog.String("body", "leak-2"), slog.String("id", "m1")))

	// Assert
	if bytes.Contains(buf.Bytes(), []byte("leak-")) {
		t.Fatalf("a value leaked into the log line: %s", buf.String())
	}
	mail, _ := decode(t, &buf)["mail"].(map[string]any)
	if mail["id"] != "m1" || mail["body"] != Redacted {
		t.Errorf("mail group = %v, want the id kept and the body redacted", mail)
	}
}

func TestOrdinaryKeysAreLeftAlone(t *testing.T) {
	// Arrange
	var buf bytes.Buffer

	// Act
	New(&buf, "kagami", "info").Info("hello", slog.String("addr", "127.0.0.1:9090"), slog.String("message_id", "m1"), slog.String("draft_id", "d1"))

	// Assert
	m := decode(t, &buf)
	if m["addr"] != "127.0.0.1:9090" || m["message_id"] != "m1" || m["draft_id"] != "d1" {
		t.Errorf("record = %v, want ids and addresses untouched", m)
	}
}
