package proxy

import (
	"context"
	"net/http"
	"net/http/httptest"
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
