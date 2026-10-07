package server

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/0xHoaxen/shogun/pkg/config"
)

// sample returns the metric of a family whose labels are all given, or nil.
func sample(t *testing.T, family string, labels map[string]string) *dto.Metric {
	t.Helper()
	families, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	for _, f := range families {
		if f.GetName() != family {
			continue
		}
		for _, m := range f.GetMetric() {
			if matches(m, labels) {
				return m
			}
		}
	}
	return nil
}

func matches(m *dto.Metric, labels map[string]string) bool {
	found := 0
	for _, l := range m.GetLabel() {
		if want, ok := labels[l.GetName()]; ok && want == l.GetValue() {
			found++
		}
	}
	return found == len(labels)
}

func requests(t *testing.T, method, code string) float64 {
	t.Helper()
	m := sample(t, "shogun_grpc_requests_total", map[string]string{"method": method, "code": code})
	if m == nil {
		return 0
	}
	return m.GetCounter().GetValue()
}

func callUnary(t *testing.T, method string, err error) {
	t.Helper()
	handler := func(context.Context, any) (any, error) { return nil, err }
	_, got := metricsUnary()(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: method}, handler)
	if !errors.Is(got, err) {
		t.Fatalf("interceptor returned %v, want the handler's %v", got, err)
	}
}

func TestMetricsUnaryCountsCallsUnderTheirStatusCode(t *testing.T) {
	// Arrange
	const method = "/shogun.test.v1.Metrics/Unary"

	// Act
	callUnary(t, method, nil)
	callUnary(t, method, nil)
	callUnary(t, method, status.Error(codes.Internal, "boom"))

	// Assert
	if got := requests(t, method, "OK"); got != 2 {
		t.Errorf("OK calls = %v, want 2", got)
	}
	if got := requests(t, method, "Internal"); got != 1 {
		t.Errorf("Internal calls = %v, want 1", got)
	}
}

func TestMetricsUnaryTimesEveryCall(t *testing.T) {
	// Arrange
	const method = "/shogun.test.v1.Metrics/Timed"

	// Act
	callUnary(t, method, nil)
	callUnary(t, method, status.Error(codes.NotFound, "gone"))

	// Assert
	m := sample(t, "shogun_grpc_request_duration_seconds", map[string]string{"method": method})
	if m == nil || m.GetHistogram().GetSampleCount() != 2 {
		t.Fatalf("duration samples = %v, want 2", m)
	}
}

func TestMetricsStreamCountsWhenTheStreamEnds(t *testing.T) {
	// Arrange
	const method = "/shogun.test.v1.Metrics/Stream"
	handler := func(any, grpc.ServerStream) error { return status.Error(codes.Unavailable, "down") }

	// Act
	err := metricsStream()(nil, nil, &grpc.StreamServerInfo{FullMethod: method}, handler)

	// Assert
	if status.Code(err) != codes.Unavailable {
		t.Fatalf("stream error = %v, want Unavailable", err)
	}
	if got := requests(t, method, "Unavailable"); got != 1 {
		t.Errorf("Unavailable streams = %v, want 1", got)
	}
}

func TestMetricsLeaveHealthProbesOut(t *testing.T) {
	// Arrange
	const method = "/grpc.health.v1.Health/Check"

	// Act
	callUnary(t, method, nil)

	// Assert
	if got := requests(t, method, "OK"); got != 0 {
		t.Errorf("health probes counted = %v, want 0", got)
	}
}

// staticGauge is a collector of one gauge.
type staticGauge struct{ desc *prometheus.Desc }

func newStaticGauge(name string) staticGauge {
	return staticGauge{desc: prometheus.NewDesc(name, "A fixed value for a test.", nil, nil)}
}

func (g staticGauge) Describe(ch chan<- *prometheus.Desc) { ch <- g.desc }
func (g staticGauge) Collect(ch chan<- prometheus.Metric) {
	ch <- prometheus.MustNewConstMetric(g.desc, prometheus.GaugeValue, 42)
}

func scrape(t *testing.T, addr string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+addr+"/metrics", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func TestWithCollectorServesItsMetricsOnlyWhileRunning(t *testing.T) {
	// Arrange
	collector := newStaticGauge("shogun_test_collector_value")

	// Act: serve with the collector, then shut down.
	r := start(t, config.Base{Environment: config.EnvLocal}, nil, WithCollector(collector))
	served := strings.Contains(scrape(t, r.httpAddr), "shogun_test_collector_value 42")
	r.stop(t)

	// Assert
	if !served {
		t.Error("/metrics does not show the collector's gauge while Run is serving")
	}
	if err := prometheus.Register(collector); err != nil {
		t.Errorf("collector still registered after Run returned: %v", err)
	}
	prometheus.Unregister(collector)
}

func TestWithCollectorKeepsServingWhenARegistrationFails(t *testing.T) {
	// Arrange: the same collector is already registered.
	collector := newStaticGauge("shogun_test_duplicate_value")
	prometheus.MustRegister(collector)
	defer prometheus.Unregister(collector)

	// Act
	r := start(t, config.Base{Environment: config.EnvLocal}, nil, WithCollector(collector))
	body := scrape(t, r.httpAddr)
	r.stop(t)

	// Assert: the service runs, and the outside registration survives Run.
	if !strings.Contains(body, "shogun_test_duplicate_value 42") {
		t.Error("/metrics lost the gauge")
	}
	if err := prometheus.Register(collector); err == nil {
		t.Error("Run unregistered a collector it did not register")
	}
}
