// Package logger builds the JSON slog logger used by every service.
package logger

import (
	"context"
	"io"
	"log/slog"
	"strings"

	"github.com/0xHoaxen/shogun/pkg/version"
)

type attrsKey struct{}

// Redacted replaces the value of an attribute whose key names something that
// must never be logged.
const Redacted = "[redacted]"

// sensitiveKeys are attribute keys, compared in lower case, whose values are
// message text, credentials or addresses. A service that needs to say which
// message or account it means logs its id instead.
var sensitiveKeys = map[string]struct{}{
	"body": {}, "subject": {}, "snippet": {}, "prompt": {},
	"email": {}, "recipient": {}, "recipients": {},
	"token": {}, "access_token": {}, "refresh_token": {}, "id_token": {}, "api_key": {}, "hanko": {},
	"password": {}, "secret": {}, "authorization": {}, "cookie": {}, "set-cookie": {},
}

// redact is a slog ReplaceAttr: it hides the value of a sensitive key wherever
// it appears, including inside groups. It looks at keys only, so a value passed
// whole under another key (a map or struct in slog.Any) is not inspected.
func redact(_ []string, a slog.Attr) slog.Attr {
	if _, hide := sensitiveKeys[strings.ToLower(a.Key)]; hide {
		return slog.String(a.Key, Redacted)
	}
	return a
}

// New returns a JSON logger that stamps every record with the service name and
// version, plus any attributes added to the context with WithAttrs. Attributes
// whose key names message text, a credential or an address are redacted.
func New(w io.Writer, service, level string) *slog.Logger {
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(level)); err != nil {
		lvl = slog.LevelInfo
	}
	base := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: lvl, ReplaceAttr: redact})
	return slog.New(contextHandler{Handler: base}).With(
		slog.String("service", service),
		slog.String("version", version.Version),
	)
}

// WithAttrs returns a context whose logs, written with the *Context methods,
// carry attrs (for example request_id or trace_id).
func WithAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	existing, _ := ctx.Value(attrsKey{}).([]slog.Attr)
	merged := make([]slog.Attr, 0, len(existing)+len(attrs))
	merged = append(merged, existing...)
	merged = append(merged, attrs...)
	return context.WithValue(ctx, attrsKey{}, merged)
}

type contextHandler struct{ slog.Handler }

func (h contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if attrs, ok := ctx.Value(attrsKey{}).([]slog.Attr); ok {
		r.AddAttrs(attrs...)
	}
	return h.Handler.Handle(ctx, r)
}

func (h contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return contextHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h contextHandler) WithGroup(name string) slog.Handler {
	return contextHandler{Handler: h.Handler.WithGroup(name)}
}
