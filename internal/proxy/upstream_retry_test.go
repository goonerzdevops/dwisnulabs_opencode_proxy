package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"opencode-proxy/internal/config"
)

func TestOverload429RetrySuccess(t *testing.T) {
	var attempts atomic.Int32

	cfg := config.Default()
	cfg.AntiAbuseEnabled = false
	cfg.Proxies = []string{"direct"}

	up := NewUpstream(cfg, nil)
	up.pool.mu.Lock()
	up.pool.nodes[0].tlsClient = nil
	up.pool.nodes[0].client = &http.Client{
		Transport: &mockRoundTripper{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				att := attempts.Add(1)
				rec := httptest.NewRecorder()
				if att < 3 {
					rec.WriteHeader(http.StatusTooManyRequests)
					rec.WriteString(`{"error":{"type":"server_error","message":"The inference service is overloaded. Please try again later."}}`)
					return rec.Result(), nil
				}
				rec.WriteHeader(http.StatusOK)
				rec.WriteString(`{"id":"chatcmpl-recovered","choices":[{"message":{"content":"recovered"}}]}`)
				return rec.Result(), nil
			},
		},
	}
	up.pool.mu.Unlock()

	prepared := &PreparedRequest{
		Path: "/test-chat",
		Body: map[string]any{"model": "big-pickle"},
	}
	fp := Fingerprint{SessionID: "ses_test_overload"}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	resp, err := up.Send(ctx, cfg, prepared, fp)
	if err != nil {
		t.Fatalf("unexpected Send error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK after overload retry, got %d", resp.StatusCode)
	}

	if attempts.Load() != 3 {
		t.Fatalf("expected exactly 3 attempts, got %d", attempts.Load())
	}
}

func TestQuota429DropsSessionImmediately(t *testing.T) {
	var attempts atomic.Int32

	cfg := config.Default()
	cfg.AntiAbuseEnabled = false
	cfg.Proxies = []string{"direct"}

	sessions := NewSessionStore(10*time.Minute, 100)
	sessionID := sessions.Stable("user-cline-1")

	// Also add translated session
	_ = TranslateSessionID("user-conv-1", "cline")

	up := NewUpstream(cfg, nil, sessions)
	up.pool.mu.Lock()
	up.pool.nodes[0].tlsClient = nil
	up.pool.nodes[0].client = &http.Client{
		Transport: &mockRoundTripper{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				attempts.Add(1)
				rec := httptest.NewRecorder()
				rec.WriteHeader(http.StatusTooManyRequests)
				rec.WriteString(`{"error":{"type":"FreeUsageLimitError","message":"Monthly free tier quota exceeded."}}`)
				return rec.Result(), nil
			},
		},
	}
	up.pool.mu.Unlock()

	prepared := &PreparedRequest{
		Path: "/test-chat",
		Body: map[string]any{"model": "big-pickle"},
	}
	fp := Fingerprint{SessionID: sessionID}

	resp, err := up.Send(context.Background(), cfg, prepared, fp)
	if err != nil {
		t.Fatalf("unexpected Send error: %v", err)
	}
	defer resp.Body.Close()

	// Quota error should NOT retry
	if attempts.Load() != 1 {
		t.Fatalf("expected exactly 1 attempt for quota 429, got %d", attempts.Load())
	}
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429, got %d", resp.StatusCode)
	}

	// Verify session was dropped from store
	sessions.mu.Lock()
	_, found := sessions.entries["user-cline-1"]
	sessions.mu.Unlock()
	if found {
		t.Errorf("expected session %q to be dropped from SessionStore", sessionID)
	}
}

func TestForbidden403DropsSessionImmediately(t *testing.T) {
	var attempts atomic.Int32

	cfg := config.Default()
	cfg.AntiAbuseEnabled = false
	cfg.Proxies = []string{"direct"}

	sessions := NewSessionStore(10*time.Minute, 100)
	sessionID := sessions.Stable("user-cursor-2")

	up := NewUpstream(cfg, nil, sessions)
	up.pool.mu.Lock()
	up.pool.nodes[0].tlsClient = nil
	up.pool.nodes[0].client = &http.Client{
		Transport: &mockRoundTripper{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				attempts.Add(1)
				rec := httptest.NewRecorder()
				rec.WriteHeader(http.StatusForbidden)
				rec.WriteString(`{"error":{"type":"FreeTierError","message":"Access forbidden."}}`)
				return rec.Result(), nil
			},
		},
	}
	up.pool.mu.Unlock()

	prepared := &PreparedRequest{
		Path: "/test-chat",
		Body: map[string]any{"model": "big-pickle"},
	}
	fp := Fingerprint{SessionID: sessionID}

	resp, err := up.Send(context.Background(), cfg, prepared, fp)
	if err != nil {
		t.Fatalf("unexpected Send error: %v", err)
	}
	defer resp.Body.Close()

	if attempts.Load() != 1 {
		t.Fatalf("expected exactly 1 attempt, got %d", attempts.Load())
	}
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", resp.StatusCode)
	}

	sessions.mu.Lock()
	_, found := sessions.entries["user-cursor-2"]
	sessions.mu.Unlock()
	if found {
		t.Errorf("expected session %q to be dropped from SessionStore", sessionID)
	}
}

func TestOverload429ContextCancellation(t *testing.T) {
	cfg := config.Default()
	cfg.AntiAbuseEnabled = false
	cfg.Proxies = []string{"direct"}

	up := NewUpstream(cfg, nil)
	up.pool.mu.Lock()
	up.pool.nodes[0].tlsClient = nil
	up.pool.nodes[0].client = &http.Client{
		Transport: &mockRoundTripper{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				rec := httptest.NewRecorder()
				rec.WriteHeader(http.StatusTooManyRequests)
				rec.WriteString(`{"error":{"type":"server_error","message":"The inference service is overloaded. Please try again later."}}`)
				return rec.Result(), nil
			},
		},
	}
	up.pool.mu.Unlock()

	prepared := &PreparedRequest{
		Path: "/test-chat",
		Body: map[string]any{"model": "big-pickle"},
	}

	// Cancel after 200ms during backoff
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_, err := up.Send(ctx, cfg, prepared, Fingerprint{SessionID: "ses_abort"})
	if err == nil {
		t.Fatalf("expected context cancellation error, got nil")
	}
}

func TestHandlerToolConcealRetryOn429(t *testing.T) {
	mgr, err := config.NewManager(filepath.Join(t.TempDir(), "config.json"))
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	srv := NewServer(mgr, nil)
	defer srv.Close()

	var attempts atomic.Int32
	var seenSessions [2]string
	var seenTools [2][]string

	srv.upstream.pool.mu.Lock()
	srv.upstream.pool.nodes[0].tlsClient = nil
	srv.upstream.pool.nodes[0].client = &http.Client{
		Transport: &mockRoundTripper{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				idx := attempts.Add(1) - 1
				raw, _ := io.ReadAll(req.Body)
				var body map[string]any
				_ = json.Unmarshal(raw, &body)

				if idx < 2 {
					seenSessions[idx] = req.Header.Get("x-opencode-session")
					if tools, ok := body["tools"].([]any); ok {
						for _, item := range tools {
							name := item.(map[string]any)["function"].(map[string]any)["name"].(string)
							seenTools[idx] = append(seenTools[idx], name)
						}
					}
				}

				rec := httptest.NewRecorder()
				if idx == 0 {
					rec.WriteHeader(http.StatusTooManyRequests)
					rec.WriteString(`{"type":"error","error":{"type":"FreeUsageLimitError","message":"Free usage limit reached."}}`)
					return rec.Result(), nil
				}
				rec.Header().Set("Content-Type", "text/event-stream")
				rec.WriteHeader(http.StatusOK)
				rec.WriteString("data: {\"id\":\"chatcmpl-ok\",\"object\":\"chat.completion.chunk\",\"created\":1,\"model\":\"glm-5.3-flash-free\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"tool_calls\":[{\"index\":0,\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"fn_1\",\"arguments\":\"{\\\"command\\\":\\\"ls\\\"}\"}}]}}]}\n\n")
				rec.WriteString("data: [DONE]\n\n")
				return rec.Result(), nil
			},
		},
	}
	srv.upstream.pool.mu.Unlock()

	reqBody := map[string]any{
		"model":  "glm-5.3-flash-free",
		"stream": true,
		"messages": []any{
			map[string]any{"role": "user", "content": "list files"},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "execute_command",
					"description": "Run CLI command",
					"parameters":  map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}},
				},
			},
		},
	}
	payload, _ := json.Marshal(reqBody)
	httpReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(payload))
	httpReq.Header.Set("Authorization", "Bearer sk-opencode-free")
	httpReq.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httpReq)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK after tool-conceal retry, got %d: %s", rec.Code, rec.Body.String())
	}
	if attempts.Load() != 2 {
		t.Fatalf("expected 2 upstream attempts, got %d", attempts.Load())
	}
	if seenSessions[0] == "" || seenSessions[1] == "" || seenSessions[0] == seenSessions[1] {
		t.Errorf("expected fresh session on retry, got %q then %q", seenSessions[0], seenSessions[1])
	}
	if seenTools[0][0] != "execute_command" {
		t.Errorf("attempt 1 should send execute_command, got %v", seenTools[0])
	}
	if seenTools[1][0] != "fn_1" {
		t.Errorf("attempt 2 should send fn_1, got %v", seenTools[1])
	}
	if !strings.Contains(rec.Body.String(), `"name":"execute_command"`) {
		t.Errorf("expected SSE output to restore original tool name execute_command, got: %s", rec.Body.String())
	}
}
