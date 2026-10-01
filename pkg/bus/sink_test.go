package bus

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	eventsv1 "github.com/0xHoaxen/shogun/gen/go/shogun/events/v1"
)

func newTestSink(t *testing.T, handlers map[string]Handler, logger *slog.Logger) *SinkServer {
	t.Helper()
	// The pool is never used by these tests; it only has to be non-nil.
	s, err := NewSinkServer(&pgxpool.Pool{}, handlers, logger)
	if err != nil {
		t.Fatalf("new sink: %v", err)
	}
	return s
}

func TestSinkAcknowledgesUnknownTypeAndLogsWithoutPayload(t *testing.T) {
	var buf bytes.Buffer
	s := newTestSink(t, nil, slog.New(slog.NewJSONHandler(&buf, nil)))

	_, err := s.Deliver(context.Background(), &eventsv1.DeliverRequest{Envelope: &eventsv1.Envelope{
		Id: "e1", Type: "mystery.happened", Source: "kagami", Subject: "secret-subject",
	}})
	if err != nil {
		t.Fatalf("deliver: %v", err)
	}
	logged := buf.String()
	if !strings.Contains(logged, "mystery.happened") || !strings.Contains(logged, "e1") {
		t.Fatalf("log missing type or id: %s", logged)
	}
	if strings.Contains(logged, "secret-subject") {
		t.Fatalf("log must not contain envelope content: %s", logged)
	}
}

func TestSinkRejectsMalformedEnvelope(t *testing.T) {
	s := newTestSink(t, nil, nil)
	for name, req := range map[string]*eventsv1.DeliverRequest{
		"nil request": nil,
		"no envelope": {},
		"no id":       {Envelope: &eventsv1.Envelope{Type: "a.b"}},
		"no type":     {Envelope: &eventsv1.Envelope{Id: "x"}},
	} {
		_, err := s.Deliver(context.Background(), req)
		if status.Code(err) != codes.InvalidArgument {
			t.Errorf("%s: code = %v, want InvalidArgument", name, status.Code(err))
		}
	}
}

func TestNewSinkServerValidatesArguments(t *testing.T) {
	noop := func(context.Context, pgx.Tx, *eventsv1.Envelope) error { return nil }
	if _, err := NewSinkServer(nil, nil, nil); err == nil {
		t.Error("nil pool: want error")
	}
	if _, err := NewSinkServer(&pgxpool.Pool{}, map[string]Handler{"": noop}, nil); err == nil {
		t.Error("empty type: want error")
	}
	if _, err := NewSinkServer(&pgxpool.Pool{}, map[string]Handler{"a.b": nil}, nil); err == nil {
		t.Error("nil handler: want error")
	}
}

func TestGRPCBusUnknownConsumerIsError(t *testing.T) {
	b := NewGRPCBus(nil)
	if err := b.Deliver(context.Background(), "taiko", &eventsv1.Envelope{Id: "x"}); err == nil {
		t.Fatal("want error for unconfigured consumer")
	}
}
