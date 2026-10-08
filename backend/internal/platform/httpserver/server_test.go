package httpserver

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modura-dev/modura/backend/internal/api/generated"
	"github.com/modura-dev/modura/backend/internal/platform/config"
)

func TestHealthEndpoints(t *testing.T) {
	cfg := config.HTTP{Address: ":0", ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second, ShutdownTimeout: time.Second, MaxHeaderBytes: 1024}
	server := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, path := range []string{"/api/livez", "/api/readyz"} {
		recorder := httptest.NewRecorder()
		request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, path, nil)
		server.Handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusOK {
			t.Fatalf("GET %s status = %d, want %d", path, recorder.Code, http.StatusOK)
		}
	}
}

func TestGeneratedParameterErrorsAreNonLeakingProblems(t *testing.T) {
	cfg := config.HTTP{Address: ":0", ReadTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second, ShutdownTimeout: time.Second, MaxHeaderBytes: 1024}
	server := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/platform/tenants/not-a-uuid/suspend", nil)
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusBadRequest)
	}
	if response.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("content type = %q", response.Header().Get("Content-Type"))
	}
	var problem generated.Problem
	if err := json.NewDecoder(response.Body).Decode(&problem); err != nil {
		t.Fatal(err)
	}
	if problem.Title != "invalid request" || problem.Status != http.StatusBadRequest {
		t.Fatalf("problem = %+v", problem)
	}
}

func testServerConfig() config.HTTP {
	return config.HTTP{Address: ":0", ReadTimeout: time.Second, ReadHeaderTimeout: time.Second, WriteTimeout: time.Second, IdleTimeout: time.Second, ShutdownTimeout: time.Second, MaxHeaderBytes: 1024, MaxBodyBytes: 64}
}

func TestRequestBodyLimitRejectsOversizedPayloads(t *testing.T) {
	server := New(testServerConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	body := strings.NewReader(`{"token":"` + strings.Repeat("x", 200) + `"}`)
	request := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/auth/password-resets", body)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestCORSPolicyIsAllowlistOnly(t *testing.T) {
	cfg := testServerConfig()
	cfg.AllowedOrigins = []string{"https://admin.example.com"}
	server := New(cfg, slog.New(slog.NewTextHandler(io.Discard, nil)))

	allowed := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/livez", nil)
	allowed.Header.Set("Origin", "https://admin.example.com")
	allowedResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(allowedResponse, allowed)
	if allowedResponse.Header().Get("Access-Control-Allow-Origin") != "https://admin.example.com" {
		t.Fatalf("allowlisted origin got no CORS header")
	}

	denied := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/livez", nil)
	denied.Header.Set("Origin", "https://evil.example.com")
	deniedResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(deniedResponse, denied)
	if deniedResponse.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("unlisted origin received CORS header")
	}

	preflight := httptest.NewRequestWithContext(context.Background(), http.MethodOptions, "/api/livez", nil)
	preflight.Header.Set("Origin", "https://admin.example.com")
	preflight.Header.Set("Access-Control-Request-Method", "DELETE")
	preflightResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(preflightResponse, preflight)
	if preflightResponse.Code != http.StatusNoContent || preflightResponse.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Fatalf("preflight status=%d methods=%q", preflightResponse.Code, preflightResponse.Header().Get("Access-Control-Allow-Methods"))
	}

	empty := New(testServerConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	anyOrigin := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/livez", nil)
	anyOrigin.Header.Set("Origin", "https://admin.example.com")
	anyOriginResponse := httptest.NewRecorder()
	empty.Handler.ServeHTTP(anyOriginResponse, anyOrigin)
	if anyOriginResponse.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("empty allowlist emitted CORS headers")
	}
}

func TestTraceContextPropagatesW3CTraceparent(t *testing.T) {
	server := New(testServerConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/livez", nil)
	request.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	response := httptest.NewRecorder()
	server.Handler.ServeHTTP(response, request)
	traceparent := response.Header().Get("traceparent")
	if !strings.HasPrefix(traceparent, "00-4bf92f3577b34da6a3ce929d0e0e4736-") {
		t.Fatalf("traceparent = %q, want same trace id", traceparent)
	}

	fresh := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/livez", nil)
	freshResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(freshResponse, fresh)
	if len(freshResponse.Header().Get("traceparent")) != 55 {
		t.Fatalf("generated traceparent = %q", freshResponse.Header().Get("traceparent"))
	}
}

func TestMetricsEndpointExposesPrometheusText(t *testing.T) {
	server := New(testServerConfig(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/api/livez", nil)
	server.Handler.ServeHTTP(httptest.NewRecorder(), request)

	metricsRequest := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil)
	metricsResponse := httptest.NewRecorder()
	server.Handler.ServeHTTP(metricsResponse, metricsRequest)
	body := metricsResponse.Body.String()
	if metricsResponse.Header().Get("Content-Type") != contentType {
		t.Fatalf("content type = %q", metricsResponse.Header().Get("Content-Type"))
	}
	for _, marker := range []string{"modura_http_requests_total", "modura_http_request_duration_seconds_bucket", `route="/api/livez"`, "modura_http_uptime_seconds"} {
		if !strings.Contains(body, marker) {
			t.Fatalf("metrics output missing %q", marker)
		}
	}
}
