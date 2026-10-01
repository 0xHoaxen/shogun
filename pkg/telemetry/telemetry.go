// Package telemetry configures OpenTelemetry tracing and metrics.
package telemetry

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.37.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/0xHoaxen/shogun/pkg/config"
	"github.com/0xHoaxen/shogun/pkg/version"
)

// Setup installs the global W3C propagator and, when cfg.OTLPEndpoint is set,
// OTLP gRPC trace and metric providers. With no endpoint it installs nothing
// beyond the propagator, so instrumentation is a no-op, and returns a nil
// error. The returned shutdown flushes and stops the providers.
func Setup(ctx context.Context, cfg config.Base) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{},
	))
	if cfg.OTLPEndpoint == "" {
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		attribute.String(string(semconv.ServiceNameKey), cfg.Service),
		attribute.String(string(semconv.ServiceVersionKey), version.Version),
		attribute.String(string(semconv.DeploymentEnvironmentNameKey), cfg.Environment),
	))
	if err != nil {
		return nil, fmt.Errorf("telemetry: resource: %w", err)
	}

	traceExp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpointURL(endpointURL(cfg.OTLPEndpoint)))
	if err != nil {
		return nil, fmt.Errorf("telemetry: trace exporter: %w", err)
	}
	metricExp, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpointURL(endpointURL(cfg.OTLPEndpoint)))
	if err != nil {
		return nil, errors.Join(
			fmt.Errorf("telemetry: metric exporter: %w", err),
			traceExp.Shutdown(ctx),
		)
	}

	tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithBatcher(traceExp))
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)))
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)

	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}, nil
}

// endpointURL accepts "host:port" as well as a full URL.
func endpointURL(endpoint string) string {
	for _, p := range []string{"http://", "https://"} {
		if len(endpoint) >= len(p) && endpoint[:len(p)] == p {
			return endpoint
		}
	}
	return "http://" + endpoint
}

// Traceparent returns the W3C traceparent of the span active in ctx, or ""
// when there is none.
func Traceparent(ctx context.Context) string {
	if !trace.SpanContextFromContext(ctx).IsValid() {
		return ""
	}
	carrier := propagation.MapCarrier{}
	propagation.TraceContext{}.Inject(ctx, carrier)
	return carrier.Get("traceparent")
}

// TraceID returns the trace id of the span active in ctx, or "" when none.
func TraceID(ctx context.Context) string {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return ""
	}
	return sc.TraceID().String()
}
