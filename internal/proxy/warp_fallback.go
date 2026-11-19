package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"opencode-proxy/internal/config"
)

// WarpState represents the lifecycle state of the lazy WARP IPv6 fallback engine.
type WarpState string

const (
	WarpStateDisabled  WarpState = "DISABLED"
	WarpStateDormant   WarpState = "DORMANT"
	WarpStateStarting  WarpState = "STARTING"
	WarpStateActive    WarpState = "ACTIVE"
	WarpStateHopping   WarpState = "HOPPING"
	WarpStateExhausted WarpState = "EXHAUSTED"
)

// WarpTunnelLauncher abstracts starting a single wireproxy instance (replaceable in unit tests).
type WarpTunnelLauncher func(ctx context.Context, cfg config.Config, store *WarpStore, dev *WarpDevice, endpoint string) (*WarpTunnelInstance, error)

// WarpFallbackManager coordinates lazy activation, single-flight epoch hopping,
// burn-list enforcement, and automatic idle/recovery shutdown of wireproxy.
type WarpFallbackManager struct {
	store  *WarpStore
	logger *log.Logger

	hopMu sync.Mutex // serializes tunnel transitions (Single-Flight Epoch Lock)

	mu             sync.Mutex
	state          WarpState
	active         *WarpTunnelInstance
	generation     uint64
	endpointIdx    int
	lastUsedAt     time.Time
	lastError      string
	exhaustedUntil time.Time
	consecutive403 int
	loggedGen      uint64
	closed         bool
	stopCh         chan struct{}

	launcherFn WarpTunnelLauncher
}

// NewWarpFallbackManager creates a lazy WARP IPv6 fallback manager.
// In normal operation it spawns zero child processes until Fallback is triggered.
func NewWarpFallbackManager(baseDir string, logger *log.Logger) *WarpFallbackManager {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	m := &WarpFallbackManager{
		store:      NewWarpStore(baseDir),
		logger:     logger,
		state:      WarpStateDormant,
		stopCh:     make(chan struct{}),
		launcherFn: startWireproxyTunnel,
	}
	go m.reaperLoop()
	return m
}

// Store returns the underlying WarpStore.
func (m *WarpFallbackManager) Store() *WarpStore {
	return m.store
}

// ShouldUseFallback reports whether new requests should be routed directly to WARP IPv6
// because the primary direct egress is currently in cooldown.
// If primary has recovered and warp_auto_return_direct is enabled, it automatically stops WARP.
func (m *WarpFallbackManager) ShouldUseFallback(cfg config.Config, pool *ProxyPool) bool {
	if !cfg.WarpFallbackEnabled {
		m.mu.Lock()
		hasActive := m.active != nil
		m.mu.Unlock()
		if hasActive {
			m.Stop("disabled_in_config")
		}
		return false
	}

	inCooldown := pool != nil && pool.IsPrimaryInCooldown()
	if inCooldown {
		return true
	}

	// Primary direct is healthy!
	if cfg.WarpAutoReturnDirect {
		m.mu.Lock()
		hasActive := m.active != nil
		m.mu.Unlock()
		if hasActive {
			m.logger.Printf("[warp-fallback] primary direct egress recovered from cooldown; stopping WARP tunnel and returning to direct")
			m.Stop("direct_recovered")
		}
		return false
	}

	m.mu.Lock()
	hasActive := m.active != nil
	m.mu.Unlock()
	return hasActive
}

// Send executes an upstream request through the WARP IPv6 fallback tunnel, transparently
// hopping across clean /64 prefixes on 429 quota or 403 errors up to cfg.WarpMaxHops.
func (m *WarpFallbackManager) Send(
	ctx context.Context,
	cfg config.Config,
	reqFactory func(Fingerprint) (*http.Request, error),
	fp Fingerprint,
	sessionStore *SessionStore,
) (*http.Response, error) {
	m.mu.Lock()
	if !time.Now().After(m.exhaustedUntil) {
		rem := time.Until(m.exhaustedUntil).Round(time.Second)
		lastErr := m.lastError
		m.mu.Unlock()
		return nil, fmt.Errorf("WARP IPv6 fallback temporarily exhausted (%s, cooldown %s left)", lastErr, rem)
	}
	m.mu.Unlock()

	maxHops := cfg.WarpMaxHops
	if maxHops <= 0 {
		maxHops = 6
	}

	var lastResp *http.Response
	var failedGen uint64
	currentFP := fp

	for hop := 0; hop <= maxHops; hop++ {
		tun, err := m.ensureOrRotateTunnel(ctx, cfg, hop > 0, failedGen)
		if err != nil {
			if lastResp != nil {
				return lastResp, nil
			}
			return nil, err
		}
		failedGen = tun.Generation

		req, err := reqFactory(currentFP)
		if err != nil {
			return nil, err
		}

		t0 := time.Now()
		resp, err := m.doThroughTunnel(ctx, tun, req)
		latencyMs := time.Since(t0).Milliseconds()
		if err != nil {
			m.logger.Printf("[warp-fallback] network error on gen=%d (%s): %v", tun.Generation, tun.Trace.Prefix64, err)
			m.setLastError(err.Error())
			if hop < maxHops {
				continue
			}
			return nil, err
		}

		if resp.StatusCode == http.StatusOK {
			m.markSuccess(tun, latencyMs)
			if lastResp != nil {
				_ = lastResp.Body.Close()
			}
			return resp, nil
		}

		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()
		resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		// 1. Transient server overload (NOT an IP quota ban!): do not burn /64, return to caller for backoff retry
		if resp.StatusCode == http.StatusTooManyRequests && isOverload429(bodyBytes) {
			if lastResp != nil {
				_ = lastResp.Body.Close()
			}
			return resp, nil
		}

		// 2. Non-quota/non-403 error (e.g. 400/500): return directly
		if resp.StatusCode != http.StatusTooManyRequests && resp.StatusCode != http.StatusForbidden {
			if lastResp != nil {
				_ = lastResp.Body.Close()
			}
			return resp, nil
		}

		// 3. Quota 429 or 403 on this WARP /64: burn prefix, log audit, mint fresh SessionID, and hop!
		fallbackBurn := time.Duration(cfg.WarpBurnHours) * time.Hour
		if fallbackBurn <= 0 {
			fallbackBurn = 12 * time.Hour
		}
		burnDur, burnSrc := ParseRetryAfter(resp.Header, fallbackBurn)
		reason := classifyRejectionReason(resp.StatusCode, bodyBytes)

		m.store.MarkBurned(tun.Trace.IP, tun.Trace.Prefix64, tun.Trace.Colo, resp.StatusCode, reason, burnDur, burnSrc)
		m.store.AppendLog(WarpLogEntry{
			Event:         "hop_burned",
			Endpoint:      tun.Trace.Endpoint,
			IP:            tun.Trace.IP,
			Prefix64:      tun.Trace.Prefix64,
			Colo:          tun.Trace.Colo,
			HTTPStatus:    resp.StatusCode,
			RetryAfterSec: int(burnDur.Seconds()),
			ExpireSource:  burnSrc,
			Reason:        reason,
			HandshakeMs:   tun.Trace.HandshakeMs,
			LatencyMs:     latencyMs,
		})
		m.logger.Printf("[warp-fallback] burned %s (%s) on HTTP %d (%s, expires in %s via %s)",
			tun.Trace.Prefix64, tun.Trace.IP, resp.StatusCode, reason, burnDur.Round(time.Second), burnSrc)

		if currentFP.SessionID != "" {
			InvalidateSession(currentFP.SessionID, sessionStore)
		}
		currentFP.SessionID = GenerateSessionID(time.Now().UnixMilli())

		if lastResp != nil {
			_ = lastResp.Body.Close()
		}
		lastResp = resp

		// Circuit breaker: 3 consecutive 403 across distinct /64 prefixes
		if resp.StatusCode == http.StatusForbidden {
			m.mu.Lock()
			m.consecutive403++
			c403 := m.consecutive403
			if c403 >= 3 {
				m.state = WarpStateExhausted
				m.lastError = "3 consecutive HTTP 403 Forbidden responses across WARP IPv6 prefixes"
				m.exhaustedUntil = time.Now().Add(15 * time.Minute)
			}
			m.mu.Unlock()
			if c403 >= 3 {
				m.Stop("circuit_breaker_403")
				return lastResp, nil
			}
		} else {
			m.mu.Lock()
			m.consecutive403 = 0
			m.mu.Unlock()
		}
	}

	m.mu.Lock()
	m.state = WarpStateExhausted
	m.lastError = fmt.Sprintf("exhausted %d WARP IPv6 hops without finding quota-free /64", maxHops)
	m.mu.Unlock()
	return lastResp, nil
}

// ManualHop forces a fresh rotation to a new unburned IPv6 /64 prefix (triggered by Dashboard POST /api/warp/hop).
func (m *WarpFallbackManager) ManualHop(ctx context.Context, cfg config.Config) (*WarpTraceInfo, error) {
	gen := m.currentGeneration()
	tun, err := m.rotateFromGeneration(ctx, cfg, gen)
	if err != nil {
		return nil, err
	}
	info := tun.Trace
	return &info, nil
}

// Stop terminates any running wireproxy tunnel and returns state to DORMANT.
func (m *WarpFallbackManager) Stop(reason string) {
	m.hopMu.Lock()
	defer m.hopMu.Unlock()

	m.mu.Lock()
	old := m.active
	m.active = nil
	if m.state != WarpStateExhausted || reason == "manual_stop" || reason == "direct_recovered" || reason == "disabled_in_config" {
		m.state = WarpStateDormant
		if reason == "manual_stop" || reason == "direct_recovered" {
			m.exhaustedUntil = time.Time{}
			m.lastError = ""
		}
	}
	m.mu.Unlock()

	if old != nil {
		m.logger.Printf("[warp-fallback] stopping wireproxy tunnel gen=%d (%s) reason=%s", old.Generation, old.Trace.Prefix64, reason)
		old.Stop()
	}
}

// Close shuts down background reaper and terminates any active wireproxy process.
func (m *WarpFallbackManager) Close() {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.closed = true
	close(m.stopCh)
	m.mu.Unlock()

	m.Stop("server_shutdown")
}

// Status builds the comprehensive status payload for GET /api/warp/status and GET /health.
func (m *WarpFallbackManager) Status(cfg config.Config, pool *ProxyPool) map[string]any {
	directInfo := map[string]any{
		"status":            "healthy",
		"healthy":           true,
		"reset_at":          "",
		"reset_in_seconds":  0,
		"reset_source":      "",
		"last_error":        "",
	}
	if pool != nil {
		directInfo = pool.PrimaryDirectStatus()
	}

	m.mu.Lock()
	state := m.state
	if !cfg.WarpFallbackEnabled && m.active == nil {
		state = WarpStateDisabled
	}
	active := m.active
	gen := m.generation
	lastUsed := m.lastUsedAt
	lastErr := m.lastError
	m.mu.Unlock()

	activeRoute := "direct"
	if active != nil && state == WarpStateActive {
		activeRoute = "warp_ipv6"
	} else if dHealthy, ok := directInfo["healthy"].(bool); ok && !dHealthy && cfg.WarpFallbackEnabled {
		activeRoute = "warp_ipv6"
	}

	warpInfo := map[string]any{
		"state":                    string(state),
		"active_ip":                "",
		"active_prefix64":          "",
		"colo":                     "",
		"endpoint":                 "",
		"local_socks_port":         0,
		"generation":               gen,
		"handshake_ms":             0,
		"connected_since":          "",
		"idle_shutdown_in_seconds": 0,
		"wireproxy_ready":          m.isWireproxyBinaryPresent(cfg),
		"device_registered":        m.store.HasDevice(),
		"last_error":               lastErr,
	}

	if active != nil {
		warpInfo["active_ip"] = active.Trace.IP
		warpInfo["active_prefix64"] = active.Trace.Prefix64
		warpInfo["colo"] = active.Trace.Colo
		warpInfo["endpoint"] = active.Trace.Endpoint
		warpInfo["local_socks_port"] = active.Trace.LocalPort
		warpInfo["handshake_ms"] = active.Trace.HandshakeMs
		warpInfo["connected_since"] = active.ConnectedAt.Format(time.RFC3339)

		idleDur := time.Duration(cfg.WarpIdleShutdownMinutes) * time.Minute
		if idleDur <= 0 {
			idleDur = 30 * time.Minute
		}
		refTime := lastUsed
		if refTime.IsZero() {
			refTime = active.ConnectedAt
		}
		rem := int(idleDur.Seconds() - time.Since(refTime).Seconds())
		if rem < 0 {
			rem = 0
		}
		warpInfo["idle_shutdown_in_seconds"] = rem
	}

	burnedActive := m.store.ActiveBurned()
	return map[string]any{
		"enabled":              cfg.WarpFallbackEnabled,
		"active_route":         activeRoute,
		"direct":               directInfo,
		"warp":                 warpInfo,
		"burned_active":        burnedActive,
		"burned_active_count":  len(burnedActive),
		"stats":                m.store.Stats(),
	}
}

func (m *WarpFallbackManager) isWireproxyBinaryPresent(cfg config.Config) bool {
	if p := strings.TrimSpace(cfg.WarpWireproxyPath); p != "" {
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return true
		}
	}
	binName := "wireproxy"
	if runtime.GOOS == "windows" {
		binName = "wireproxy.exe"
	}
	if info, err := os.Stat(filepath.Join(m.store.DataDir(), binName)); err == nil && !info.IsDir() && info.Size() > 0 {
		return true
	}
	return false
}

func (m *WarpFallbackManager) currentGeneration() uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.generation
}

func (m *WarpFallbackManager) setLastError(msg string) {
	m.mu.Lock()
	m.lastError = msg
	m.mu.Unlock()
}

func (m *WarpFallbackManager) ensureOrRotateTunnel(ctx context.Context, cfg config.Config, forceRotate bool, seenGen uint64) (*WarpTunnelInstance, error) {
	if !forceRotate {
		m.mu.Lock()
		if m.active != nil {
			if _, burned := m.store.IsBurned(m.active.Trace.Prefix64); !burned {
				tun := m.active
				m.mu.Unlock()
				return tun, nil
			}
		}
		m.mu.Unlock()
	}
	return m.rotateFromGeneration(ctx, cfg, seenGen)
}

// rotateFromGeneration performs a Single-Flight Epoch Hop: if another concurrent goroutine
// already advanced m.generation past seenGen and the new tunnel is clean, it reuses that tunnel.
func (m *WarpFallbackManager) rotateFromGeneration(ctx context.Context, cfg config.Config, seenGen uint64) (*WarpTunnelInstance, error) {
	m.hopMu.Lock()
	defer m.hopMu.Unlock()

	m.mu.Lock()
	if m.active != nil && m.generation > seenGen {
		if _, burned := m.store.IsBurned(m.active.Trace.Prefix64); !burned {
			tun := m.active
			m.mu.Unlock()
			return tun, nil
		}
	}
	prevIP := ""
	if m.active != nil {
		prevIP = m.active.Trace.IP
		m.state = WarpStateHopping
	} else {
		m.state = WarpStateStarting
	}
	m.mu.Unlock()

	dev, err := m.store.EnsureDevice(ctx)
	if err != nil {
		m.mu.Lock()
		m.state = WarpStateDormant
		m.lastError = err.Error()
		m.mu.Unlock()
		return nil, err
	}

	endpoints := cfg.WarpEndpoints
	if len(endpoints) == 0 {
		endpoints = config.DefaultWarpEndpoints()
	}
	maxProbes := cfg.WarpMaxTraceProbes
	if maxProbes <= 0 {
		maxProbes = 4
	}

	identicalIPCount := 0
	var lastProbeErr error

	for probe := 0; probe < maxProbes; probe++ {
		m.mu.Lock()
		ep := endpoints[m.endpointIdx%len(endpoints)]
		m.endpointIdx = (m.endpointIdx + 1) % len(endpoints)
		launcher := m.launcherFn
		m.mu.Unlock()

		candidate, err := launcher(ctx, cfg, m.store, dev, ep)
		if err != nil {
			lastProbeErr = err
			m.logger.Printf("[warp-fallback] probe #%d/%d on %s failed: %v", probe+1, maxProbes, ep, err)
			continue
		}

		// Anti-looping check: detect system-wide VPN forcing identical egress IP
		if prevIP != "" && candidate.Trace.IP == prevIP {
			identicalIPCount++
			if identicalIPCount >= 2 {
				candidate.Stop()
				vpnErr := fmt.Errorf("possible system-wide VPN conflict: egress IP %s did not change after multiple rotations", prevIP)
				m.mu.Lock()
				m.state = WarpStateExhausted
				m.lastError = vpnErr.Error()
				m.mu.Unlock()
				return nil, vpnErr
			}
		} else {
			prevIP = candidate.Trace.IP
		}

		// Skip if this /64 prefix is already in active burn-list!
		if entry, burned := m.store.IsBurned(candidate.Trace.Prefix64); burned {
			rem := time.Until(entry.ExpiresAt).Round(time.Second)
			m.logger.Printf("[warp-fallback] probe #%d/%d landed on burned prefix %s (%s left), skipping without hitting opencode.ai",
				probe+1, maxProbes, candidate.Trace.Prefix64, rem)
			candidate.Stop()
			lastProbeErr = fmt.Errorf("landed on burned prefix %s", candidate.Trace.Prefix64)
			continue
		}

		// Clean IPv6 /64 found! Perform atomic swap.
		m.mu.Lock()
		m.generation++
		candidate.Generation = m.generation
		old := m.active
		m.active = candidate
		m.state = WarpStateActive
		m.lastUsedAt = time.Now()
		m.lastError = ""
		m.exhaustedUntil = time.Time{}
		m.mu.Unlock()

		if old != nil {
			// Stop old tunnel asynchronously after swap
			go old.Stop()
		}

		m.logger.Printf("[warp-fallback] active WARP IPv6 tunnel gen=%d ip=%s prefix=%s colo=%s ep=%s (%dms)",
			candidate.Generation, candidate.Trace.IP, candidate.Trace.Prefix64, candidate.Trace.Colo, candidate.Trace.Endpoint, candidate.Trace.HandshakeMs)
		return candidate, nil
	}

	if lastProbeErr == nil {
		lastProbeErr = fmt.Errorf("failed to acquire clean WARP IPv6 prefix after %d probes", maxProbes)
	}
	m.mu.Lock()
	if m.active == nil {
		m.state = WarpStateDormant
	}
	m.lastError = lastProbeErr.Error()
	m.mu.Unlock()
	return nil, lastProbeErr
}

func (m *WarpFallbackManager) doThroughTunnel(ctx context.Context, tun *WarpTunnelInstance, req *http.Request) (*http.Response, error) {
	if tun.tlsClient != nil {
		freq, err := toFHTTPRequest(ctx, req)
		if err != nil {
			return nil, err
		}
		fresp, err := tun.tlsClient.Do(freq)
		if err != nil {
			return nil, err
		}
		return toStandardResponse(fresp), nil
	}
	if tun.stdClient != nil {
		return tun.stdClient.Do(req.WithContext(ctx))
	}
	return nil, fmt.Errorf("warp tunnel has no HTTP client configured")
}

func (m *WarpFallbackManager) markSuccess(tun *WarpTunnelInstance, latencyMs int64) {
	m.mu.Lock()
	m.lastUsedAt = time.Now()
	m.consecutive403 = 0
	shouldLog := m.loggedGen != tun.Generation
	if shouldLog {
		m.loggedGen = tun.Generation
	}
	m.mu.Unlock()

	if shouldLog {
		m.store.AppendLog(WarpLogEntry{
			Event:       "hop_active",
			Endpoint:    tun.Trace.Endpoint,
			IP:          tun.Trace.IP,
			Prefix64:    tun.Trace.Prefix64,
			Colo:        tun.Trace.Colo,
			HTTPStatus:  http.StatusOK,
			HandshakeMs: tun.Trace.HandshakeMs,
			LatencyMs:   latencyMs,
		})
	}
}

func (m *WarpFallbackManager) reaperLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.stopCh:
			return
		case <-ticker.C:
			m.mu.Lock()
			active := m.active
			lastUsed := m.lastUsedAt
			m.mu.Unlock()
			if active == nil {
				continue
			}
			ref := lastUsed
			if ref.IsZero() {
				ref = active.ConnectedAt
			}
			if time.Since(ref) > 30*time.Minute {
				m.logger.Printf("[warp-fallback] tunnel idle for >30m; shutting down wireproxy to free memory")
				m.Stop("idle_timeout")
			}
		}
	}
}

func classifyRejectionReason(status int, body []byte) string {
	var env struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err == nil && env.Error.Type != "" {
		return env.Error.Type
	}
	if status == http.StatusForbidden {
		return "FreeTierError"
	}
	return "FreeUsageLimitError"
}
