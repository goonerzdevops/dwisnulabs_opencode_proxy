package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"opencode-proxy/internal/config"
)

type kiloMockRoundTripper func(*http.Request) (*http.Response, error)

func (f kiloMockRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestIsKiloModelAndClean(t *testing.T) {
	cases := []struct {
		model   string
		isKilo  bool
		cleaned string
	}{
		{"openrouter/free", true, "openrouter/free"},
		{"kilo-auto/free", true, "kilo-auto/free"},
		{"kilo:openrouter/free", true, "openrouter/free"},
		{"kilo/nvidia/nemotron-3-ultra-550b-a55b:free", true, "nvidia/nemotron-3-ultra-550b-a55b:free"},
		{"nvidia/nemotron-3-ultra-550b-a55b:free", true, "nvidia/nemotron-3-ultra-550b-a55b:free"},
		{"stepfun/step-3.7-flash:free", true, "stepfun/step-3.7-flash:free"},
		{"poolside/laguna-xs-2.1:free", true, "poolside/laguna-xs-2.1:free"},
		{"custom/model:free", true, "custom/model:free"},
		{"custom/model/free", true, "custom/model/free"},
		{"big-pickle", false, "big-pickle"},
		{"claude-3-5-sonnet", false, "claude-3-5-sonnet"},
		{"gpt-4o", false, "gpt-4o"},
	}

	for _, c := range cases {
		if got := IsKiloModel(c.model); got != c.isKilo {
			t.Errorf("IsKiloModel(%q) = %v; want %v", c.model, got, c.isKilo)
		}
		if got := CleanKiloModel(c.model); got != c.cleaned {
			t.Errorf("CleanKiloModel(%q) = %q; want %q", c.model, got, c.cleaned)
		}
	}
}

func TestKiloFetchModelsFallback(t *testing.T) {
	service := NewKiloService(nil)
	service.SetHTTPClient(&http.Client{
		Transport: kiloMockRoundTripper(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusBadGateway,
				Body:       io.NopCloser(strings.NewReader("upstream error")),
				Header:     make(http.Header),
			}, nil
		}),
	})

	models := service.FetchModels(context.Background(), true)
	if len(models) < len(KnownKiloModels) {
		t.Fatalf("expected at least %d fallback models, got %d", len(KnownKiloModels), len(models))
	}
	found := false
	for _, m := range models {
		if m.ID == "openrouter/free" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected openrouter/free in fallback models")
	}
}

func TestKiloFetchModelsParsing(t *testing.T) {
	service := NewKiloService(nil)
	mockCatalog := `{
		"data": [
			{"id": "kilo-auto/free", "name": "Auto Free", "isFree": true, "created": 1700000000},
			{"id": "kilo-auto/frontier", "name": "Frontier Paid", "isFree": false, "created": 1700000000},
			{"id": "nvidia/nemotron-3-ultra-550b-a55b:free", "name": "Nemotron 550B", "isFree": true, "created": 1700000000},
			{"id": "paid-model/v1", "name": "Paid Model", "isFree": false, "created": 1700000000}
		]
	}`

	service.SetHTTPClient(&http.Client{
		Transport: kiloMockRoundTripper(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(mockCatalog)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}),
	})

	models := service.FetchModels(context.Background(), true)
	ids := make(map[string]bool)
	for _, m := range models {
		ids[m.ID] = true
	}

	if !ids["kilo-auto/free"] {
		t.Errorf("expected kilo-auto/free in parsed models")
	}
	if !ids["nvidia/nemotron-3-ultra-550b-a55b:free"] {
		t.Errorf("expected nvidia/nemotron-3-ultra-550b-a55b:free in parsed models")
	}
	if ids["paid-model/v1"] {
		t.Errorf("paid-model/v1 should not be included in free models")
	}
}

func setupTestServer(t *testing.T, roundTripper kiloMockRoundTripper) (*Server, func()) {
	tmpDir, err := os.MkdirTemp("", "opencode_kilo_test_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	cfgPath := filepath.Join(tmpDir, "config.json")
	mgr, err := config.NewManager(cfgPath)
	if err != nil {
		t.Fatalf("failed to init config manager: %v", err)
	}

	cfg := mgr.Get()
	cfg.KiloEnabled = true
	cfg.KiloDefaultModel = "openrouter/free"
	if err := mgr.Update(cfg); err != nil {
		t.Fatalf("failed to update config: %v", err)
	}

	logger := log.New(io.Discard, "", 0)
	server := NewServer(mgr, logger)
	if roundTripper != nil {
		server.kilo.SetHTTPClient(&http.Client{Transport: roundTripper})
	}

	cleanup := func() {
		server.Close()
		_ = os.RemoveAll(tmpDir)
	}
	return server, cleanup
}

func TestKiloChatForwardingNonStream(t *testing.T) {
	mockResponse := `{
		"id": "chatcmpl-kilo-test",
		"object": "chat.completion",
		"created": 1700000000,
		"model": "openrouter/free",
		"choices": [
			{
				"index": 0,
				"message": {
					"role": "assistant",
					"content": "Hello from Kilo Free!"
				},
				"finish_reason": "stop"
			}
		],
		"usage": {
			"prompt_tokens": 10,
			"completion_tokens": 5,
			"total_tokens": 15
		}
	}`

	server, cleanup := setupTestServer(t, func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "" {
			t.Errorf("expected no Authorization header for keyless Kilo request, got %q", r.Header.Get("Authorization"))
		}
		if r.URL.String() != KiloChatURL {
			t.Errorf("expected URL %s, got %s", KiloChatURL, r.URL.String())
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(mockResponse)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	})
	defer cleanup()

	payload := map[string]any{
		"model": "openrouter/free",
		"messages": []map[string]string{
			{"role": "user", "content": "Hi"},
		},
		"stream": false,
	}
	rawPayload, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v2/kilo/chat/completions", bytes.NewReader(rawPayload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.Handler().ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	var resData map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&resData); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	choices, ok := resData["choices"].([]any)
	if !ok || len(choices) == 0 {
		t.Fatalf("expected choices in response, got %v", resData)
	}
}

func TestKiloChatForwardingStream(t *testing.T) {
	mockSSE := "data: {\"choices\":[{\"delta\":{\"content\":\"Hello \"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"world!\"}}]}\n\ndata: [DONE]\n\n"

	server, cleanup := setupTestServer(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       io.NopCloser(strings.NewReader(mockSSE)),
			Header:     http.Header{"Content-Type": []string{"text/event-stream"}},
		}, nil
	})
	defer cleanup()

	payload := map[string]any{
		"model": "kilo:openrouter/free",
		"messages": []map[string]string{
			{"role": "user", "content": "Hi"},
		},
		"stream": true,
	}
	rawPayload, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(rawPayload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.Handler().ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	bodyStr := string(body)
	if !strings.Contains(bodyStr, "Hello ") || !strings.Contains(bodyStr, "[DONE]") {
		t.Fatalf("unexpected stream body: %s", bodyStr)
	}
}

func TestKiloChatRateLimit(t *testing.T) {
	rateLimitErr := `{"error":{"message":"Rate limit exceeded (200 requests/hour)","type":"rate_limit_exceeded"}}`

	server, cleanup := setupTestServer(t, func(r *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Body:       io.NopCloser(strings.NewReader(rateLimitErr)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	})
	defer cleanup()

	payload := map[string]any{
		"model": "openrouter/free",
		"messages": []map[string]string{
			{"role": "user", "content": "Hi"},
		},
	}
	rawPayload, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/v2/kilo/chat/completions", bytes.NewReader(rawPayload))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	server.Handler().ServeHTTP(w, req)

	resp := w.Result()
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected status 429, got %d", resp.StatusCode)
	}
}

