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
