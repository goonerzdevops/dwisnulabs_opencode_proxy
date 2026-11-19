package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"opencode-proxy/internal/config"
)

func mockWarpDevice() *WarpDevice {
	var reg WarpRegResponse
	reg.ID = "dev-test-1"
	reg.WarpEnabled = true
	reg.Config.Interface.Addresses.V4 = "172.16.0.2"
	reg.Config.Interface.Addresses.V6 = "2606:4700:110:8ec8:d51e:899b:1c14:f2ab"
	reg.Config.Peers = append(reg.Config.Peers, struct {
		PublicKey string `json:"public_key"`
		Endpoint  struct {
			V4   string `json:"v4"`
			V6   string `json:"v6"`
			Host string `json:"host"`
		} `json:"endpoint"`
	}{
		PublicKey: "TEST_MOCK_PEER_PUBLIC_KEY",
	})
	return &WarpDevice{
		PrivateKey: "TEST_MOCK_PRIVATE_KEY",
		Reg:        reg,
		CreatedAt:  time.Now(),
	}
}

func TestParseRetryAfter(t *testing.T) {
	fallback := 12 * time.Hour

	// 1. Exact integer seconds from opencode.ai 429 response
	h1 := http.Header{}
	h1.Set("Retry-After", "44616")
	d1, src1 := ParseRetryAfter(h1, fallback)
	if d1 != 44616*time.Second || src1 != "header:Retry-After" {
		t.Fatalf("expected 44616s from header:Retry-After, got %v (%s)", d1, src1)
	}

	// 2. X-RateLimit-Reset delta seconds
	h2 := http.Header{}
	h2.Set("X-RateLimit-Reset", "3600")
	d2, src2 := ParseRetryAfter(h2, fallback)
	if d2 != 3600*time.Second || src2 != "header:X-RateLimit-Reset" {
		t.Fatalf("expected 3600s from X-RateLimit-Reset, got %v (%s)", d2, src2)
	}

	// 3. Fallback when header missing
	d3, src3 := ParseRetryAfter(http.Header{}, fallback)
	if d3 != fallback || src3 != "fallback:12h0m0s" {
		t.Fatalf("expected fallback 12h, got %v (%s)", d3, src3)
	}
}

func TestWarpStoreBurnAndAutoPrune(t *testing.T) {
	store := NewWarpStore(t.TempDir())

	// Mark a prefix burned with short 150ms TTL
	p64, ok := ExtractIPv6Prefix64("2a09:bac1:34a0:10::279:36")
	if !ok || p64 != "2a09:bac1:34a0:10::/64" {
		t.Fatalf("unexpected prefix64: %s (ok=%v)", p64, ok)
	}

	store.MarkBurned("2a09:bac1:34a0:10::279:36", p64, "SIN", 429, "FreeUsageLimitError", 150*time.Millisecond, "header:Retry-After")
	store.AppendLog(WarpLogEntry{
		Event:      "hop_burned",
		IP:         "2a09:bac1:34a0:10::279:36",
		Prefix64:   p64,
		Colo:       "SIN",
		HTTPStatus: 429,
		Reason:     "FreeUsageLimitError",
	})

	if _, burned := store.IsBurned(p64); !burned {
		t.Fatalf("expected %s to be burned immediately after MarkBurned", p64)
	}
	if active := store.ActiveBurned(); len(active) != 1 {
		t.Fatalf("expected 1 active burned entry, got %d", len(active))
	}

	// Wait for TTL to expire -> real-time auto-prune must treat it as clean immediately!
	time.Sleep(220 * time.Millisecond)
	if _, burned := store.IsBurned(p64); burned {
		t.Fatalf("expected %s to be auto-pruned after TTL expiry", p64)
	}
	if active := store.ActiveBurned(); len(active) != 0 {
		t.Fatalf("expected 0 active burned entries after expiry, got %d", len(active))
	}

	// Historical audit log counter must remain intact even after burn-list expiry!
	stats := store.Stats()
	if stats.TotalBurned429 != 1 || stats.TotalHops != 1 {
		t.Fatalf("expected historical stats to remain 1 burned / 1 hop, got %+v", stats)
	}
}

func TestOverload429DoesNotCooldownDirectOrTriggerWarp(t *testing.T) {
	cfg := config.Default()
	cfg.AntiAbuseEnabled = false
	cfg.WarpFallbackEnabled = true

	var directHits, warpLaunches atomic.Int32

	up := NewUpstream(cfg, nil)
	wm := NewWarpFallbackManager(t.TempDir(), nil)
	defer wm.Close()
	wm.store.registerFn = func(ctx context.Context) (*WarpDevice, error) {
		return mockWarpDevice(), nil
	}
	wm.launcherFn = func(ctx context.Context, cfg config.Config, store *WarpStore, dev *WarpDevice, endpoint string) (*WarpTunnelInstance, error) {
		warpLaunches.Add(1)
		return nil, fmt.Errorf("should never be called on overload 429")
	}
	up.SetWarpManager(wm)

	up.pool.mu.Lock()
	up.pool.nodes[0].tlsClient = nil
	up.pool.nodes[0].client = &http.Client{
		Transport: &mockRoundTripper{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				hit := directHits.Add(1)
				rec := httptest.NewRecorder()
				if hit == 1 {
					rec.WriteHeader(http.StatusTooManyRequests)
					rec.WriteString(`{"error":{"type":"server_error","message":"The inference service is overloaded. Please try again later."}}`)
					return rec.Result(), nil
				}
				rec.WriteHeader(http.StatusOK)
				rec.WriteString(`{"choices":[{"message":{"content":"OK"}}]}`)
				return rec.Result(), nil
			},
		},
	}
	up.pool.mu.Unlock()

	prepared := &PreparedRequest{
		Path: "/inference/openai/v1/chat/completions",
		Body: map[string]any{"model": "big-pickle"},
	}
	resp, err := up.Send(context.Background(), cfg, prepared, Fingerprint{SessionID: GenerateSessionID(time.Now().UnixMilli())})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 after overload retry, got %d", resp.StatusCode)
	}
	if directHits.Load() != 2 {
		t.Fatalf("expected 2 hits on direct, got %d", directHits.Load())
	}
	if warpLaunches.Load() != 0 {
		t.Fatalf("expected 0 WARP launches on transient overload 429, got %d", warpLaunches.Load())
	}
	if up.pool.IsPrimaryInCooldown() {
		t.Fatalf("expected direct node NOT to be in cooldown after transient overload 429")
	}
}

func TestDirectQuota429TriggersWarpFallbackAndAutoReturnsToDirect(t *testing.T) {
	cfg := config.Default()
	cfg.AntiAbuseEnabled = false
	cfg.WarpFallbackEnabled = true
	cfg.WarpAutoReturnDirect = true

	var directHits, warpHits, warpStopped atomic.Int32
	var seenDirectSession, seenWarpSession string

	up := NewUpstream(cfg, nil)
	wm := NewWarpFallbackManager(t.TempDir(), nil)
	defer wm.Close()
	wm.store.registerFn = func(ctx context.Context) (*WarpDevice, error) {
		return mockWarpDevice(), nil
	}
	wm.launcherFn = func(ctx context.Context, cfg config.Config, store *WarpStore, dev *WarpDevice, endpoint string) (*WarpTunnelInstance, error) {
		client := &http.Client{
			Transport: &mockRoundTripper{
				roundTrip: func(req *http.Request) (*http.Response, error) {
					warpHits.Add(1)
					seenWarpSession = req.Header.Get("x-opencode-session")
					rec := httptest.NewRecorder()
					rec.WriteHeader(http.StatusOK)
					rec.WriteString(`{"choices":[{"message":{"content":"OK from WARP IPv6"}}]}`)
					return rec.Result(), nil
				},
			},
		}
		return &WarpTunnelInstance{
			Trace: WarpTraceInfo{
				IP:          "2a09:bac5:3a05:1d05::2e4:98",
				Prefix64:    "2a09:bac5:3a05:1d05::/64",
				Colo:        "SIN",
				WarpStatus:  "on",
				Endpoint:    endpoint,
				LocalPort:   42101,
				ProxyURL:    "socks5://127.0.0.1:42101",
				HandshakeMs: 1200,
			},
			ConnectedAt: time.Now(),
			stdClient:   client,
			stopFn: func() {
				warpStopped.Add(1)
			},
		}, nil
	}
	up.SetWarpManager(wm)

	up.pool.mu.Lock()
	up.pool.nodes[0].tlsClient = nil
	up.pool.nodes[0].client = &http.Client{
		Transport: &mockRoundTripper{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				hit := directHits.Add(1)
				seenDirectSession = req.Header.Get("x-opencode-session")
				rec := httptest.NewRecorder()
				if hit == 1 {
					rec.Header().Set("Retry-After", "1")
					rec.WriteHeader(http.StatusTooManyRequests)
					rec.WriteString(`{"type":"error","error":{"type":"FreeUsageLimitError","message":"Free usage limit reached."}}`)
					return rec.Result(), nil
				}
				rec.WriteHeader(http.StatusOK)
				rec.WriteString(`{"choices":[{"message":{"content":"OK from Direct after reset"}}]}`)
				return rec.Result(), nil
			},
		},
	}
	up.pool.mu.Unlock()

	prepared := &PreparedRequest{
		Path: "/inference/openai/v1/chat/completions",
		Body: map[string]any{"model": "big-pickle"},
	}

	// Request 1: Direct returns 429 (Retry-After: 1s) -> transparently fails over to WARP IPv6 and returns 200!
	initialSess := GenerateSessionID(time.Now().UnixMilli() - 5000)
	resp1, err := up.Send(context.Background(), cfg, prepared, Fingerprint{SessionID: initialSess})
	if err != nil {
		t.Fatalf("Req 1 error: %v", err)
	}
	body1, _ := io.ReadAll(resp1.Body)
	resp1.Body.Close()

	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("expected Req 1 to succeed via WARP fallback (200), got %d", resp1.StatusCode)
	}
	if string(body1) != `{"choices":[{"message":{"content":"OK from WARP IPv6"}}]}` {
		t.Fatalf("unexpected body1: %s", string(body1))
	}
	if seenWarpSession == "" || seenWarpSession == seenDirectSession {
		t.Fatalf("expected fresh SessionID on WARP failover, got direct=%s warp=%s", seenDirectSession, seenWarpSession)
	}

	// Verify status reflects direct in cooldown and WARP active
	st := wm.Status(cfg, up.pool)
	if st["active_route"] != "warp_ipv6" {
		t.Fatalf("expected active_route=warp_ipv6, got %v", st["active_route"])
	}

	// Request 2 (immediately while direct is still in 1s cooldown): skips direct, goes straight to WARP
	resp2, err := up.Send(context.Background(), cfg, prepared, Fingerprint{SessionID: GenerateSessionID(time.Now().UnixMilli())})
	if err != nil {
		t.Fatalf("Req 2 error: %v", err)
	}
	resp2.Body.Close()
	if directHits.Load() != 1 || warpHits.Load() != 2 {
		t.Fatalf("expected direct=1 warp=2 during cooldown, got direct=%d warp=%d", directHits.Load(), warpHits.Load())
	}

	// Wait for direct Retry-After (1s) to expire -> Request 3 must auto-stop WARP and return to Direct!
	time.Sleep(1150 * time.Millisecond)
	resp3, err := up.Send(context.Background(), cfg, prepared, Fingerprint{SessionID: GenerateSessionID(time.Now().UnixMilli())})
	if err != nil {
		t.Fatalf("Req 3 error: %v", err)
	}
	body3, _ := io.ReadAll(resp3.Body)
	resp3.Body.Close()

	if directHits.Load() != 2 {
		t.Fatalf("expected Req 3 to use direct after cooldown expiry (directHits=2), got %d", directHits.Load())
	}
	if warpStopped.Load() < 1 {
		t.Fatalf("expected WARP tunnel to be stopped automatically when direct recovered")
	}
	if string(body3) != `{"choices":[{"message":{"content":"OK from Direct after reset"}}]}` {
		t.Fatalf("unexpected body3: %s", string(body3))
	}
}

func TestWarpHopSkipsBurnedPrefixAndRotatesOn429(t *testing.T) {
	cfg := config.Default()
	cfg.WarpFallbackEnabled = true
	cfg.WarpMaxHops = 4
	cfg.WarpMaxTraceProbes = 4

	wm := NewWarpFallbackManager(t.TempDir(), nil)
	defer wm.Close()
	wm.store.registerFn = func(ctx context.Context) (*WarpDevice, error) {
		return mockWarpDevice(), nil
	}

	// Pre-burn prefix #1 so the probe must skip it without hitting opencode.ai!
	wm.store.MarkBurned("2a09:bac1:34a0:10::1", "2a09:bac1:34a0:10::/64", "SIN", 429, "FreeUsageLimitError", time.Hour, "header:Retry-After")

	var launchSeq atomic.Int32
	var upstreamHits atomic.Int32

	prefixes := []struct {
		ip     string
		prefix string
		status int
	}{
		{"2a09:bac1:34a0:10::1", "2a09:bac1:34a0:10::/64", 429},   // Probe 1: pre-burned -> skipped at trace!
		{"2a09:bac1:34c0:10::2", "2a09:bac1:34c0:10::/64", 429},   // Probe 2: hits upstream -> returns 429 -> burned & triggers hop!
		{"2a09:bac5:3a02:15f::3", "2a09:bac5:3a02:15f::/64", 200}, // Probe 3: hits upstream -> returns 200 OK!
	}

	wm.launcherFn = func(ctx context.Context, cfg config.Config, store *WarpStore, dev *WarpDevice, endpoint string) (*WarpTunnelInstance, error) {
		idx := int(launchSeq.Add(1)) - 1
		if idx >= len(prefixes) {
			idx = len(prefixes) - 1
		}
		p := prefixes[idx]
		client := &http.Client{
			Transport: &mockRoundTripper{
				roundTrip: func(req *http.Request) (*http.Response, error) {
					upstreamHits.Add(1)
					rec := httptest.NewRecorder()
					if p.status == 429 {
						rec.Header().Set("Retry-After", "7200")
						rec.WriteHeader(http.StatusTooManyRequests)
						rec.WriteString(`{"type":"error","error":{"type":"FreeUsageLimitError","message":"Free usage limit reached."}}`)
						return rec.Result(), nil
					}
					rec.WriteHeader(http.StatusOK)
					rec.WriteString(`{"choices":[{"message":{"content":"OK after hop"}}]}`)
					return rec.Result(), nil
				},
			},
		}
		return &WarpTunnelInstance{
			Trace: WarpTraceInfo{
				IP:         p.ip,
				Prefix64:   p.prefix,
				Colo:       "SIN",
				WarpStatus: "on",
				Endpoint:   endpoint,
			},
			ConnectedAt: time.Now(),
			stdClient:   client,
			stopFn:      func() {},
		}, nil
	}

	reqFactory := func(fp Fingerprint) (*http.Request, error) {
		return http.NewRequest(http.MethodPost, "https://opencode.ai/inference/openai/v1/chat/completions", nil)
	}

	resp, err := wm.Send(context.Background(), cfg, reqFactory, Fingerprint{SessionID: GenerateSessionID(time.Now().UnixMilli())}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK after hopping, got %d", resp.StatusCode)
	}
	if launchSeq.Load() != 3 {
		t.Fatalf("expected 3 tunnel launches (1 skipped pre-burned + 1 hit 429 + 1 hit 200), got %d", launchSeq.Load())
	}
	if upstreamHits.Load() != 2 {
		t.Fatalf("expected only 2 upstream hits (pre-burned prefix must be skipped!), got %d", upstreamHits.Load())
	}
	if _, burned := wm.store.IsBurned("2a09:bac1:34c0:10::/64"); !burned {
		t.Fatalf("expected second prefix 2a09:bac1:34c0:10::/64 to be marked burned after 429")
	}
}

func TestSingleFlightEpochConcurrentHop(t *testing.T) {
	cfg := config.Default()
	cfg.WarpFallbackEnabled = true

	wm := NewWarpFallbackManager(t.TempDir(), nil)
	defer wm.Close()
	wm.store.registerFn = func(ctx context.Context) (*WarpDevice, error) {
		return mockWarpDevice(), nil
	}

	var launches atomic.Int32
	wm.launcherFn = func(ctx context.Context, cfg config.Config, store *WarpStore, dev *WarpDevice, endpoint string) (*WarpTunnelInstance, error) {
		time.Sleep(60 * time.Millisecond)
		n := launches.Add(1)
		client := &http.Client{
			Transport: &mockRoundTripper{
				roundTrip: func(req *http.Request) (*http.Response, error) {
					rec := httptest.NewRecorder()
					rec.WriteHeader(http.StatusOK)
					rec.WriteString(`{"status":"ok"}`)
					return rec.Result(), nil
				},
			},
		}
		return &WarpTunnelInstance{
			Trace: WarpTraceInfo{
				IP:         fmt.Sprintf("2a09:bac5:3a05:%d::1", n),
				Prefix64:   fmt.Sprintf("2a09:bac5:3a05:%d::/64", n),
				Colo:       "SIN",
				WarpStatus: "on",
				Endpoint:   endpoint,
			},
			ConnectedAt: time.Now(),
			stdClient:   client,
			stopFn:      func() {},
		}, nil
	}

	var wg sync.WaitGroup
	const workers = 5
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			reqFactory := func(fp Fingerprint) (*http.Request, error) {
				return http.NewRequest(http.MethodPost, "https://opencode.ai/test", nil)
			}
			resp, err := wm.Send(context.Background(), cfg, reqFactory, Fingerprint{SessionID: GenerateSessionID(time.Now().UnixMilli())}, nil)
			if err != nil {
				t.Errorf("worker send err: %v", err)
				return
			}
			resp.Body.Close()
		}()
	}
	wg.Wait()

	if launches.Load() != 1 {
		t.Fatalf("expected Single-Flight Epoch Lock to launch exactly 1 tunnel for %d concurrent requests, got %d", workers, launches.Load())
	}
}

func TestWarpAPIEndpoints(t *testing.T) {
	mgr, err := config.NewManager(t.TempDir() + "/config.json")
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	srv := NewServer(mgr, nil)
	defer srv.Close()

	wm := srv.upstream.Warp()
	if wm == nil {
		t.Fatalf("expected Server to initialize WarpFallbackManager")
	}
	wm.store.registerFn = func(ctx context.Context) (*WarpDevice, error) {
		return mockWarpDevice(), nil
	}
	wm.launcherFn = func(ctx context.Context, cfg config.Config, store *WarpStore, dev *WarpDevice, endpoint string) (*WarpTunnelInstance, error) {
		return &WarpTunnelInstance{
			Trace: WarpTraceInfo{
				IP:          "2a09:bac5:3a09:1122::3c1:5",
				Prefix64:    "2a09:bac5:3a09:1122::/64",
				Colo:        "SIN",
				WarpStatus:  "on",
				Endpoint:    endpoint,
				LocalPort:   42201,
				ProxyURL:    "socks5://127.0.0.1:42201",
				HandshakeMs: 1180,
			},
			ConnectedAt: time.Now(),
			stopFn:      func() {},
		}, nil
	}

	h := srv.Handler()

	// 1. GET /api/warp/status
	req1 := httptest.NewRequest(http.MethodGet, "/api/warp/status", nil)
	rec1 := httptest.NewRecorder()
	h.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("GET /api/warp/status expected 200, got %d", rec1.Code)
	}

	// 2. POST /api/warp/hop
	req2 := httptest.NewRequest(http.MethodPost, "/api/warp/hop", nil)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("POST /api/warp/hop expected 200, got %d: %s", rec2.Code, rec2.Body.String())
	}

	// 3. Burn an entry and clear via POST /api/warp/burned/clear
	wm.Store().MarkBurned("2a09:bac1:34a0:10::9", "2a09:bac1:34a0:10::/64", "SIN", 429, "FreeUsageLimitError", time.Hour, "header:Retry-After")
	req3 := httptest.NewRequest(http.MethodPost, "/api/warp/burned/clear", nil)
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Fatalf("POST /api/warp/burned/clear expected 200, got %d", rec3.Code)
	}
	if len(wm.Store().ActiveBurned()) != 0 {
		t.Fatalf("expected burned list to be empty after clear")
	}

	// 4. POST /api/warp/stop
	req4 := httptest.NewRequest(http.MethodPost, "/api/warp/stop", nil)
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	if rec4.Code != http.StatusOK {
		t.Fatalf("POST /api/warp/stop expected 200, got %d", rec4.Code)
	}

	// 5. GET /health includes warp object
	req5 := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec5 := httptest.NewRecorder()
	h.ServeHTTP(rec5, req5)
	if rec5.Code != http.StatusOK {
		t.Fatalf("GET /health expected 200, got %d", rec5.Code)
	}
}
