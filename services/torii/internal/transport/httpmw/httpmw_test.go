package httpmw_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/0xHoaxen/shogun/services/torii/internal/transport/httpmw"
)

var ok = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

func requestFrom(remoteAddr string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	return req
}

func TestRateLimiterRejectsBeyondTheBurst(t *testing.T) {
	// Arrange
	handler := httpmw.NewRateLimiter(1, 2).Middleware(ok)
	var codes []int

	// Act
	for range 4 {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, requestFrom("192.0.2.1:5000"))
		codes = append(codes, rec.Code)
	}

	// Assert
	want := []int{http.StatusOK, http.StatusOK, http.StatusTooManyRequests, http.StatusTooManyRequests}
	for i := range want {
		if codes[i] != want[i] {
			t.Fatalf("codes = %v, want %v", codes, want)
		}
	}
}

func TestRateLimiterSetsRetryAfter(t *testing.T) {
	// Arrange
	handler := httpmw.NewRateLimiter(1, 1).Middleware(ok)
	handler.ServeHTTP(httptest.NewRecorder(), requestFrom("192.0.2.1:5000"))

	// Act
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestFrom("192.0.2.1:5001"))

	// Assert
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("status %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}
}

func TestRateLimiterTracksClientsSeparately(t *testing.T) {
	// Arrange
	handler := httpmw.NewRateLimiter(1, 1).Middleware(ok)
	handler.ServeHTTP(httptest.NewRecorder(), requestFrom("192.0.2.1:5000"))

	// Act
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestFrom("192.0.2.2:5000"))

	// Assert
	if rec.Code != http.StatusOK {
		t.Fatalf("second client status = %d, want 200", rec.Code)
	}
}

func TestLimitBodyFailsReadsPastTheCap(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		wantErr bool
	}{
		{"under the cap", "12345", false},
		{"exactly the cap", "1234567890", false},
		{"over the cap", "12345678901", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Arrange
			var readErr error
			handler := httpmw.LimitBody(10)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, readErr = io.ReadAll(r.Body)
				w.WriteHeader(http.StatusOK)
			}))
			req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))

			// Act
			handler.ServeHTTP(httptest.NewRecorder(), req)

			// Assert
			if (readErr != nil) != tt.wantErr {
				t.Fatalf("read error = %v, want error %v", readErr, tt.wantErr)
			}
		})
	}
}
