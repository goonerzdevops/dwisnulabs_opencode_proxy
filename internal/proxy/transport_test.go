package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"opencode-proxy/internal/config"
)

type mockRoundTripper struct {
	roundTrip func(req *http.Request) (*http.Response, error)
}

func (m *mockRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTrip(req)
}

func TestProxyPoolFailoverOn429(t *testing.T) {
	var srv1Hits, srv2Hits atomic.Int32

	cfg := config.Default()
	cfg.Proxies = []string{"http://proxy1:8080", "http://proxy2:8080"}
	cfg.ProxyCooldownSeconds = 60

	pool := NewProxyPool(cfg, nil)

	pool.mu.Lock()
	pool.nodes[0].tlsClient = nil
	pool.nodes[0].client = &http.Client{
		Transport: &mockRoundTripper{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				srv1Hits.Add(1)
				rec := httptest.NewRecorder()
				rec.WriteHeader(http.StatusTooManyRequests)
				rec.WriteString(`{"error":{"message":"Rate limit exceeded for IP"}}`)
				return rec.Result(), nil
			},
		},
	}
	pool.nodes[1].tlsClient = nil
	pool.nodes[1].client = &http.Client{
		Transport: &mockRoundTripper{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				srv2Hits.Add(1)
				rec := httptest.NewRecorder()
				rec.WriteHeader(http.StatusOK)
				rec.WriteString(`{"choices":[{"message":{"content":"Hello from backup node"}}]}`)
				return rec.Result(), nil
			},
		},
	}
	pool.mu.Unlock()

	reqFactory := func() (*http.Request, error) {
		return http.NewRequest(http.MethodPost, "http://example.com/test", nil)
	}

	// First request: hits proxy1 -> gets 429 -> automatically fails over to proxy2 -> gets 200 OK
	resp, route, err := pool.DoWithFailover(context.Background(), cfg, reqFactory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from failover, got %d", resp.StatusCode)
	}
	if srv1Hits.Load() != 1 {
		t.Errorf("expected 1 hit on proxy1, got %d", srv1Hits.Load())
	}
	if srv2Hits.Load() != 1 {
		t.Errorf("expected 1 hit on proxy2, got %d", srv2Hits.Load())
	}
	if route != "http://proxy2:8080" {
		t.Errorf("expected route http://proxy2:8080, got %s", route)
	}

	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"choices":[{"message":{"content":"Hello from backup node"}}]}` {
		t.Errorf("unexpected body: %s", string(body))
	}

	// Second request: proxy1 is in cooldown! NextCandidate should skip proxy1 and go straight to proxy2!
	resp2, route2, err := pool.DoWithFailover(context.Background(), cfg, reqFactory)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from proxy2, got %d", resp2.StatusCode)
	}
	if srv1Hits.Load() != 1 {
		t.Errorf("expected proxy1 to be skipped (still 1 hit), got %d", srv1Hits.Load())
	}
	if srv2Hits.Load() != 2 {
		t.Errorf("expected 2 hits on proxy2, got %d", srv2Hits.Load())
	}
	if route2 != "http://proxy2:8080" {
		t.Errorf("expected route http://proxy2:8080, got %s", route2)
	}

	// Verify status exposes cooldown
	status := pool.Status()
	if len(status) != 2 {
		t.Fatalf("expected 2 nodes in status, got %d", len(status))
	}
	if status[0]["healthy"] != false {
		t.Errorf("expected srv1 healthy=false")
	}
	if status[1]["healthy"] != true {
		t.Errorf("expected srv2 healthy=true")
	}
}

func TestProxyPoolAntiAbusePacingAndConcurrency(t *testing.T) {
	cfg := config.Default()
	cfg.Proxies = []string{"direct"}
	cfg.AntiAbuseEnabled = true
	cfg.MaxConcurrentPerNode = 1
	cfg.MinRequestIntervalMs = 150

	pool := NewProxyPool(cfg, nil)

	var activeCount atomic.Int32
	var maxActive atomic.Int32

	pool.mu.Lock()
	pool.nodes[0].tlsClient = nil
	pool.nodes[0].client = &http.Client{
		Transport: &mockRoundTripper{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				curr := activeCount.Add(1)
				defer activeCount.Add(-1)

				for {
					oldMax := maxActive.Load()
					if curr <= oldMax || maxActive.CompareAndSwap(oldMax, curr) {
						break
					}
				}

				time.Sleep(50 * time.Millisecond)
				rec := httptest.NewRecorder()
				rec.WriteHeader(http.StatusOK)
				rec.WriteString(`{"status":"ok"}`)
				return rec.Result(), nil
			},
		},
	}
	pool.mu.Unlock()

	reqFactory := func() (*http.Request, error) {
		return http.NewRequest(http.MethodPost, "http://example.com/test", nil)
	}

	// Run two requests concurrently
	start := time.Now()
	var wg sync.WaitGroup
	wg.Add(2)

	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			resp, _, err := pool.DoWithFailover(context.Background(), cfg, reqFactory)
			if err != nil {
				t.Errorf("request error: %v", err)
				return
			}
			io.ReadAll(resp.Body)
			resp.Body.Close()
		}()
	}

	wg.Wait()
	elapsed := time.Since(start)

	// Since MaxConcurrentPerNode = 1, maxActive must never exceed 1!
	if maxActive.Load() > 1 {
		t.Errorf("expected max concurrent active requests <= 1, got %d", maxActive.Load())
	}

	// Total duration must be at least req1 duration (50ms) + minInterval (150ms) + req2 duration (50ms) = 250ms
	if elapsed < 200*time.Millisecond {
		t.Errorf("expected elapsed >= 200ms due to pacing and concurrency, got %v", elapsed)
	}
}
