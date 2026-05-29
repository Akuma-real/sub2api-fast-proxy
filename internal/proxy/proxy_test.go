package proxy

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestProxyInjectsPriorityServiceTier(t *testing.T) {
	var capturedBody string
	var capturedAuth string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	handler := NewHandler(testConfig(t, upstream.URL), slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`{"model":"gpt-5.5","input":[]}`))
	req.Header.Set("Authorization", "Bearer sk-test")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if capturedAuth != "Bearer sk-test" {
		t.Fatalf("Authorization = %q", capturedAuth)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(capturedBody), &parsed); err != nil {
		t.Fatalf("upstream body is invalid JSON: %s", capturedBody)
	}
	if parsed["service_tier"] != "priority" {
		t.Fatalf("service_tier = %v, want priority; body=%s", parsed["service_tier"], capturedBody)
	}
}

func TestProxyLeavesModelsEndpointUntouched(t *testing.T) {
	var capturedBody string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		capturedBody = string(body)
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer upstream.Close()

	handler := NewHandler(testConfig(t, upstream.URL), slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodPost, "/v1/models", strings.NewReader(`{"probe":true}`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if capturedBody != `{"probe":true}` {
		t.Fatalf("body = %s", capturedBody)
	}
}

func TestProxyAddsAnthropicFastBeta(t *testing.T) {
	var capturedBeta string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedBeta = r.Header.Get("anthropic-beta")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	handler := NewHandler(testConfig(t, upstream.URL), slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"claude","messages":[]}`))
	req.Header.Set("anthropic-beta", "context-1m-2025-08-07")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(capturedBeta, "context-1m-2025-08-07") || !strings.Contains(capturedBeta, defaultAnthropicFastBeta) {
		t.Fatalf("anthropic-beta = %q", capturedBeta)
	}
}

func TestProxyRejectsInvalidJSONOnForcedPath(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("upstream should not be called")
	}))
	defer upstream.Close()

	handler := NewHandler(testConfig(t, upstream.URL), slog.New(slog.NewTextHandler(io.Discard, nil)))
	req := httptest.NewRequest(http.MethodPost, "/v1/responses", strings.NewReader(`[]`))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func testConfig(t *testing.T, rawURL string) Config {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	return Config{
		ListenAddr:         "127.0.0.1:0",
		UpstreamURL:        parsed,
		ForceServiceTier:   "priority",
		OpenAIJSONPaths:    parsePathSet(defaultOpenAIJSONPaths),
		AnthropicFastPaths: parsePathSet(defaultAnthropicFastPaths),
		AnthropicFastBeta:  defaultAnthropicFastBeta,
		MaxBodyBytes:       1 << 20,
		StrictInjection:    true,
		ReadHeaderTimeout:  timeSecond(),
		IdleTimeout:        timeSecond(),
		MaxHeaderBytes:     1 << 20,
	}
}

func timeSecond() time.Duration {
	return time.Second
}
