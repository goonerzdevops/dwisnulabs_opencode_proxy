package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

var (
	errTunnelRetiring = errors.New("warp tunnel retiring")
	errVPNConflict    = errors.New("system-wide VPN conflict")
)

// WarpSendOptions controls per-request behavior inside WarpFallbackManager.SendWithOptions.
type WarpSendOptions struct {
	// ReturnOnFirst429WithoutBurn instructs SendWithOptions to return the first non-overload
	// 429 response back to Upstream.Send without burning the active /64 prefix or hopping,
	// so Upstream.Send can first try ConcealAllCallerTools on the exact same healthy tunnel.
	ReturnOnFirst429WithoutBurn bool
}

// WarpTunnelLauncher abstracts starting a single wireproxy instance (replaceable in unit tests).
type WarpTunnelLauncher func(ctx context.Context, cfg config.Config, store *WarpStore, dev *WarpDevice, endpoint string) (*WarpTunnelInstance, error)

// WarpFallbackManager coordinates lazy activation, single-flight epoch hopping,
// 1-slot warm standby swapping, graceful in-flight draining, burn-list enforcement,
// and automatic idle/recovery shutdown of wireproxy.
type WarpFallbackManager struct {
	store  *WarpStore
	logger *log.Logger

	hopMu sync.Mutex // serializes tunnel transitions (Single-Flight Epoch Lock)

	mu                 sync.Mutex
	state              WarpState
	active             *WarpTunnelInstance
	standby            *WarpTunnelInstance
	warmStandbyEnabled bool
	warmInProgress     bool
	idleTimeout        time.Duration
	generation         uint64
	endpointIdx        int
	endpointPenalty    map[string]time.Time
	lastUsedAt         time.Time
	lastError          string
	exhaustedUntil     time.Time
	consecutive403     int
	loggedGen          uint64
	closed             bool
	stopCh             chan struct{}

	launcherFn WarpTunnelLauncher
}

// NewWarpFallbackManager creates a lazy WARP IPv6 fallback manager.
// In normal operation it spawns zero child processes until Fallback is triggered.
func NewWarpFallbackManager(baseDir string, logger *log.Logger) *WarpFallbackManager {
	if logger == nil {
		logger = log.New(io.Discard, "", 0)
	}
	m := &WarpFallbackManager{
		store:           NewWarpStore(baseDir),
		logger:          logger,
		state:           WarpStateDormant,
		idleTimeout:     30 * time.Minute,
		endpointPenalty: make(map[string]time.Time),
		stopCh:          make(chan struct{}),
		launcherFn:      startWireproxyTunnel,
	}
	go m.reaperLoop()
	return m
}

// SetWarmStandby enables or disables background pre-warming of 1 spare WARP IPv6 tunnel
// while WARP is actively serving traffic, enabling near-instant (<5ms) IPv6-to-IPv6 swaps.
func (m *WarpFallbackManager) SetWarmStandby(enabled bool) {
	m.mu.Lock()
	m.warmStandbyEnabled = enabled
	var oldStandby *WarpTunnelInstance
	if !enabled && m.standby != nil {
		oldStandby = m.standby
		m.standby = nil
	}
	m.mu.Unlock()
	if oldStandby != nil {
		oldStandby.Stop()
	}
}

// SyncConfig updates runtime parameters such as idle timeout and stops WARP when disabled.
func (m *WarpFallbackManager) SyncConfig(cfg config.Config) {
	idle := time.Duration(cfg.WarpIdleShutdownMinutes) * time.Minute
	if idle <= 0 {
		idle = 30 * time.Minute
	}
	m.mu.Lock()
	m.idleTimeout = idle
	m.mu.Unlock()

	if !cfg.WarpFallbackEnabled {
		m.Stop("disabled_in_config")
	}
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
		hasActive := m.active != nil || m.standby != nil
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
		hasActive := m.active != nil || m.standby != nil
		m.mu.Unlock()
		if hasActive {
			m.logger.Printf("[warp-fallback] primary direct egress recovered from cooldown; stopping WARP tunnel and returning to direct")
			m.store.RecordEvent(WarpLogEntry{
				Event:  "direct_recovered",
				Reason: "primary direct egress cooldown expired",
			})
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
	return m.SendWithOptions(ctx, cfg, reqFactory, fp, sessionStore, WarpSendOptions{})
}

// SendWithOptions executes an upstream request through WARP IPv6 with caller-specified options.
func (m *WarpFallbackManager) SendWithOptions(
	ctx context.Context,
	cfg config.Config,
	reqFactory func(Fingerprint) (*http.Request, error),
	fp Fingerprint,
	sessionStore *SessionStore,
	opts WarpSendOptions,
) (*http.Response, error) {
	m.mu.Lock()
	if !m.exhaustedUntil.IsZero() && !time.Now().After(m.exhaustedUntil) {
		rem := time.Until(m.exhaustedUntil).Round(time.Second)
		lastErr := m.lastError
		m.mu.Unlock()
		return nil, fmt.Errorf("WARP IPv6 fallback temporarily exhausted (%s, cooldown %s left)", lastErr, rem)
	}
	if !m.exhaustedUntil.IsZero() && time.Now().After(m.exhaustedUntil) {
		m.exhaustedUntil = time.Time{}
		if m.state == WarpStateExhausted {
			m.state = WarpStateDormant
		}
	}
	m.mu.Unlock()

	maxHops := cfg.WarpMaxHops
	if maxHops <= 0 {
		maxHops = 6
	}

	var lastResp *http.Response
	var failedGen uint64
	currentFP := fp
	rotateFailures := 0
	const maxRotateFailures = 2

	for hop := 0; hop <= maxHops; hop++ {
		tun, err := m.ensureOrRotateTunnel(ctx, cfg, hop > 0, failedGen)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, errVPNConflict) {
				if lastResp != nil {
					return lastResp, nil
				}
				return nil, err
			}
			rotateFailures++
			if rotateFailures <= maxRotateFailures && hop < maxHops {
				m.logger.Printf("[warp-fallback] rotation round %d/%d did not find clean prefix (%v); continuing retry",
					rotateFailures, maxRotateFailures, err)
				continue
			}
			if lastResp != nil {
				return lastResp, nil
			}
			return nil, err
		}
		rotateFailures = 0
		failedGen = tun.Generation

		req, err := reqFactory(currentFP)
		if err != nil {
			return nil, err
		}

		t0 := time.Now()
		resp, err := m.doThroughTunnel(ctx, tun, req)
		latencyMs := time.Since(t0).Milliseconds()
		if err != nil {
			if errors.Is(err, errTunnelRetiring) {
				// Concurrent swap retired this tunnel right before we entered; retry immediately on new tunnel.
				continue
			}
			m.logger.Printf("[warp-fallback] network error on gen=%d (%s): %v", tun.Generation, tun.Trace.Prefix64, err)
			m.setLastError(err.Error())
			if hop < maxHops {
				continue
			}
			return nil, err
		}

		if resp.StatusCode == http.StatusOK {
			m.markSuccess(tun, latencyMs, cfg)
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

		// 1b. Content/tool-description 429 guard: when payload still has unconcealed caller tools,
		// return immediately without burning this healthy /64 or hopping so Upstream.Send can
		// conceal caller tools and retry on this exact tunnel first.
		if resp.StatusCode == http.StatusTooManyRequests && opts.ReturnOnFirst429WithoutBurn {
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
	old := m.active
	m.active = nil
	m.state = WarpStateExhausted
	m.exhaustedUntil = time.Now().Add(15 * time.Second)
	m.lastError = fmt.Sprintf("exhausted %d WARP IPv6 hops without finding quota-free /64", maxHops)
	m.mu.Unlock()
	if old != nil {
		old.Retire()
	}
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

// Stop terminates any running active and standby wireproxy tunnels and returns state to DORMANT.
func (m *WarpFallbackManager) Stop(reason string) {
	m.hopMu.Lock()
	defer m.hopMu.Unlock()

	m.mu.Lock()
	old := m.active
	oldStandby := m.standby
	m.active = nil
	m.standby = nil
	if m.state != WarpStateExhausted || reason == "manual_stop" || reason == "direct_recovered" || reason == "disabled_in_config" || reason == "idle_timeout" {
		m.state = WarpStateDormant
		if reason == "manual_stop" || reason == "direct_recovered" || reason == "idle_timeout" {
			m.exhaustedUntil = time.Time{}
			m.lastError = ""
		}
	}
	m.mu.Unlock()

	if old != nil {
		m.logger.Printf("[warp-fallback] stopping wireproxy tunnel gen=%d (%s) reason=%s", old.Generation, old.Trace.Prefix64, reason)
		if reason == "server_shutdown" || reason == "manual_stop" || reason == "disabled_in_config" {
			old.Stop()
		} else {
			old.Retire()
		}
	}
	if oldStandby != nil {
		m.logger.Printf("[warp-fallback] stopping warm standby tunnel (%s) reason=%s", oldStandby.Trace.Prefix64, reason)
		oldStandby.Stop()
	}
}

// Close shuts down background reaper and terminates any active/standby wireproxy process.
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
		"status":           "healthy",
		"healthy":          true,
		"reset_at":         "",
		"reset_in_seconds": 0,
		"reset_source":     "",
		"last_error":       "",
	}
	if pool != nil {
		directInfo = pool.PrimaryDirectStatus()
	}

	m.mu.Lock()
	if m.state == WarpStateExhausted && !m.exhaustedUntil.IsZero() && time.Now().After(m.exhaustedUntil) {
		m.exhaustedUntil = time.Time{}
		m.state = WarpStateDormant
	}
	state := m.state
	if !cfg.WarpFallbackEnabled && m.active == nil {
		state = WarpStateDisabled
	}
	active := m.active
	standby := m.standby
	warmInProgress := m.warmInProgress
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
		"standby_ready":            false,
		"standby_warming":          warmInProgress,
		"standby_ip":               "",
		"standby_prefix64":         "",
		"standby_endpoint":         "",
	}

	activePrefix := ""
	if active != nil {
		activePrefix = active.Trace.Prefix64
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

	standbyPrefix := ""
	if standby != nil {
		standbyPrefix = standby.Trace.Prefix64
		warpInfo["standby_ready"] = true
		warpInfo["standby_ip"] = standby.Trace.IP
		warpInfo["standby_prefix64"] = standby.Trace.Prefix64
		warpInfo["standby_endpoint"] = standby.Trace.Endpoint
	}

	burnedActive := m.store.ActiveBurned()
	healthyIPs := m.store.HealthyList(activePrefix, standbyPrefix)
	healthyCleanCount := 0
	for _, h := range healthyIPs {
		if h.Status != "burned" {
			healthyCleanCount++
		}
	}

	return map[string]any{
		"enabled":             cfg.WarpFallbackEnabled,
		"active_route":        activeRoute,
		"direct":              directInfo,
		"warp":                warpInfo,
		"healthy_ips":         healthyIPs,
		"healthy_count":       len(healthyIPs),
		"healthy_clean_count": healthyCleanCount,
		"burned_active":       burnedActive,
		"burned_active_count": len(burnedActive),
		"recent_events":       m.store.RecentEvents(20),
		"stats":               m.store.Stats(),
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

func (m *WarpFallbackManager) pickEndpointLocked(endpoints []string) string {
	now := time.Now()
	n := len(endpoints)
	for i := 0; i < n; i++ {
		idx := (m.endpointIdx + i) % n
		ep := endpoints[idx]
		if pen, ok := m.endpointPenalty[ep]; !ok || now.After(pen) {
			delete(m.endpointPenalty, ep)
			m.endpointIdx = (idx + 1) % n
			return ep
		}
	}
	// All penalized: pick the one whose penalty expires soonest.
	bestIdx := 0
	bestExp := m.endpointPenalty[endpoints[0]]
	for i := 1; i < n; i++ {
		if exp := m.endpointPenalty[endpoints[i]]; exp.Before(bestExp) {
			bestIdx = i
			bestExp = exp
		}
	}
	m.endpointIdx = (bestIdx + 1) % n
	return endpoints[bestIdx]
}

func (m *WarpFallbackManager) penalizeEndpoint(ep string, dur time.Duration) {
	m.mu.Lock()
	m.endpointPenalty[ep] = time.Now().Add(dur)
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

// rotateFromGeneration performs a Single-Flight Epoch Hop:
//  1. If another concurrent goroutine already advanced m.generation past seenGen, reuses it.
//  2. If a clean pre-warmed standby tunnel is ready, promotes it atomically in <1ms!
//  3. Otherwise probes for a clean, unburned, non-congested /64 (racing 2 probes in parallel
//     when the burned pool is busy).
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

	// Fast path: promote pre-warmed warm standby tunnel in <1ms!
	if m.standby != nil {
		cand := m.standby
		m.standby = nil
		_, burned := m.store.IsBurned(cand.Trace.Prefix64)
		sameAsActive := m.active != nil && m.active.Trace.Prefix64 == cand.Trace.Prefix64
		if !burned && !sameAsActive {
			m.generation++
			cand.Generation = m.generation
			old := m.active
			m.active = cand
			m.state = WarpStateActive
			m.lastUsedAt = time.Now()
			m.lastError = ""
			m.exhaustedUntil = time.Time{}
			m.mu.Unlock()

			if old != nil {
				old.Retire()
			}
			m.store.RecordEvent(WarpLogEntry{
				Event:       "swap_standby",
				Endpoint:    cand.Trace.Endpoint,
				IP:          cand.Trace.IP,
				Prefix64:    cand.Trace.Prefix64,
				Colo:        cand.Trace.Colo,
				HandshakeMs: 0,
				Reason:      "instant_warm_standby_swap",
			})
			m.logger.Printf("[warp-fallback] instant standby swap -> gen=%d ip=%s prefix=%s colo=%s ep=%s",
				cand.Generation, cand.Trace.IP, cand.Trace.Prefix64, cand.Trace.Colo, cand.Trace.Endpoint)
			m.triggerStandbyWarmAsync(cfg)
			return cand, nil
		}
		m.mu.Unlock()
		cand.Stop()
		m.mu.Lock()
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
		m.recoverFailedRotationState(err.Error())
		return nil, err
	}

	endpoints := cfg.WarpEndpoints
	if len(endpoints) == 0 {
		endpoints = config.DefaultWarpEndpoints()
	}

	burnedCount := m.store.ActiveBurnedCount()
	maxProbes := cfg.WarpMaxTraceProbes
	if maxProbes <= 0 {
		maxProbes = 4
	}
	// Adaptive probe budget when the burned pool is crowded so we don't give up prematurely.
	if burnedCount >= 12 {
		boosted := maxProbes + burnedCount/5
		if boosted > 16 {
			boosted = 16
		}
		if boosted > maxProbes {
			maxProbes = boosted
		}
	}

	identicalIPCount := 0
	var lastProbeErr error

	for probe := 0; probe < maxProbes; {
		batchSize := 1
		// Engage 2-way parallel race probes once burned pool gets busy to cut rotation latency in half.
		if burnedCount >= 8 && (maxProbes-probe) >= 2 {
			batchSize = 2
		}

		candidate, err, vpnErr := m.runProbeBatch(ctx, cfg, dev, endpoints, batchSize, probe, maxProbes, &prevIP, &identicalIPCount)
		probe += batchSize
		if vpnErr != nil {
			m.mu.Lock()
			m.state = WarpStateExhausted
			m.exhaustedUntil = time.Now().Add(2 * time.Minute)
			m.lastError = vpnErr.Error()
			m.mu.Unlock()
			return nil, vpnErr
		}
		if err != nil {
			lastProbeErr = err
			if ctx.Err() != nil {
				break
			}
			continue
		}

		// Clean IPv6 /64 found! Perform atomic swap + graceful drain on old tunnel.
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
			old.Retire()
		}

		m.logger.Printf("[warp-fallback] active WARP IPv6 tunnel gen=%d ip=%s prefix=%s colo=%s ep=%s (%dms)",
			candidate.Generation, candidate.Trace.IP, candidate.Trace.Prefix64, candidate.Trace.Colo, candidate.Trace.Endpoint, candidate.Trace.HandshakeMs)
		return candidate, nil
	}

	if lastProbeErr == nil {
		lastProbeErr = fmt.Errorf("failed to acquire clean WARP IPv6 prefix after %d probes", maxProbes)
	}
	m.recoverFailedRotationState(lastProbeErr.Error())
	return nil, lastProbeErr
}

func (m *WarpFallbackManager) runProbeBatch(
	ctx context.Context,
	cfg config.Config,
	dev *WarpDevice,
	endpoints []string,
	batchSize int,
	probeOffset int,
	maxProbes int,
	prevIP *string,
	identicalIPCount *int,
) (*WarpTunnelInstance, error, error) {
	if batchSize <= 1 {
		m.mu.Lock()
		ep := m.pickEndpointLocked(endpoints)
		launcher := m.launcherFn
		m.mu.Unlock()

		candidate, err := launcher(ctx, cfg, m.store, dev, ep)
		if err != nil {
			if ctx.Err() == nil {
				m.penalizeEndpoint(ep, 2*time.Minute)
			}
			m.logger.Printf("[warp-fallback] probe #%d/%d on %s failed: %v", probeOffset+1, maxProbes, ep, err)
			return nil, err, nil
		}
		return m.validateProbeCandidate(candidate, probeOffset+1, maxProbes, prevIP, identicalIPCount)
	}

	batchCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	type probeRes struct {
		idx int
		ep  string
		tun *WarpTunnelInstance
		err error
	}
	results := make(chan probeRes, batchSize)

	for i := 0; i < batchSize; i++ {
		m.mu.Lock()
		ep := m.pickEndpointLocked(endpoints)
		launcher := m.launcherFn
		m.mu.Unlock()

		probeNum := probeOffset + i + 1
		go func(num int, endpoint string) {
			tun, err := launcher(batchCtx, cfg, m.store, dev, endpoint)
			results <- probeRes{idx: num, ep: endpoint, tun: tun, err: err}
		}(probeNum, ep)
	}

	var winner *WarpTunnelInstance
	var lastErr error
	var vpnErr error

	for i := 0; i < batchSize; i++ {
		res := <-results
		if winner != nil {
			if res.tun != nil {
				res.tun.Stop()
			}
			continue
		}
		if res.err != nil {
			if ctx.Err() == nil && batchCtx.Err() == nil {
				m.penalizeEndpoint(res.ep, 2*time.Minute)
			}
			lastErr = res.err
			m.logger.Printf("[warp-fallback] parallel probe #%d/%d on %s failed: %v", res.idx, maxProbes, res.ep, res.err)
			continue
		}
		cand, vErr, vConflict := m.validateProbeCandidate(res.tun, res.idx, maxProbes, prevIP, identicalIPCount)
		if vConflict != nil {
			vpnErr = vConflict
			cancel()
			continue
		}
		if vErr != nil {
			lastErr = vErr
			continue
		}
		winner = cand
		cancel()
	}

	if winner != nil {
		return winner, nil, nil
	}
	if vpnErr != nil {
		return nil, nil, vpnErr
	}
	return nil, lastErr, nil
}

func (m *WarpFallbackManager) validateProbeCandidate(
	candidate *WarpTunnelInstance,
	probeNum int,
	maxProbes int,
	prevIP *string,
	identicalIPCount *int,
) (*WarpTunnelInstance, error, error) {
	// Anti-looping check: detect system-wide VPN forcing identical egress IP
	if *prevIP != "" && candidate.Trace.IP == *prevIP {
		*identicalIPCount++
		if *identicalIPCount >= 2 {
			candidate.Stop()
			return nil, nil, fmt.Errorf("%w: egress IP %s did not change after multiple rotations", errVPNConflict, *prevIP)
		}
	} else {
		*prevIP = candidate.Trace.IP
	}

	// Skip if this /64 prefix is already in active burn-list!
	if entry, burned := m.store.IsBurned(candidate.Trace.Prefix64); burned {
		rem := time.Until(entry.ExpiresAt).Round(time.Second)
		m.logger.Printf("[warp-fallback] probe #%d/%d landed on burned prefix %s (%s left), skipping without hitting opencode.ai",
			probeNum, maxProbes, candidate.Trace.Prefix64, rem)
		candidate.Stop()
		return nil, fmt.Errorf("landed on burned prefix %s", candidate.Trace.Prefix64), nil
	}

	// Skip historically congested /32 blocks (e.g. 2a09:bac1::/32 with 0% HTTP 200) unless on last resort probe.
	if probeNum < maxProbes && m.store.IsBlockCongested(candidate.Trace.Prefix64) {
		m.logger.Printf("[warp-fallback] probe #%d/%d landed on congested block %s (0%% historical 200), skipping",
			probeNum, maxProbes, candidate.Trace.Prefix64)
		candidate.Stop()
		return nil, fmt.Errorf("skipped congested block %s", candidate.Trace.Prefix64), nil
	}

	return candidate, nil, nil
}

func (m *WarpFallbackManager) recoverFailedRotationState(errMsg string) {
	var burnedActive *WarpTunnelInstance
	m.mu.Lock()
	if m.active != nil {
		if _, burned := m.store.IsBurned(m.active.Trace.Prefix64); burned {
			burnedActive = m.active
			m.active = nil
			m.state = WarpStateDormant
		} else {
			m.state = WarpStateActive
		}
	} else {
		m.state = WarpStateDormant
	}
	m.lastError = errMsg
	m.mu.Unlock()
	if burnedActive != nil {
		burnedActive.Retire()
	}
}

func (m *WarpFallbackManager) triggerStandbyWarmAsync(cfg config.Config) {
	if !cfg.WarpFallbackEnabled {
		return
	}
	m.mu.Lock()
	if !m.warmStandbyEnabled || m.closed || m.active == nil || m.standby != nil || m.warmInProgress {
		m.mu.Unlock()
		return
	}
	activePrefix := m.active.Trace.Prefix64
	m.warmInProgress = true
	m.mu.Unlock()

	go func() {
		defer func() {
			m.mu.Lock()
			m.warmInProgress = false
			m.mu.Unlock()
		}()

		warmCtx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
		defer cancel()

		dev, err := m.store.EnsureDevice(warmCtx)
		if err != nil {
			return
		}
		endpoints := cfg.WarpEndpoints
		if len(endpoints) == 0 {
			endpoints = config.DefaultWarpEndpoints()
		}

		for attempt := 0; attempt < 4; attempt++ {
			m.mu.Lock()
			if m.closed || !m.warmStandbyEnabled || m.active == nil || m.standby != nil {
				m.mu.Unlock()
				return
			}
			ep := m.pickEndpointLocked(endpoints)
			launcher := m.launcherFn
			m.mu.Unlock()

			cand, err := launcher(warmCtx, cfg, m.store, dev, ep)
			if err != nil {
				m.penalizeEndpoint(ep, 2*time.Minute)
				continue
			}

			_, burned := m.store.IsBurned(cand.Trace.Prefix64)
			if burned || cand.Trace.Prefix64 == activePrefix || m.store.IsBlockCongested(cand.Trace.Prefix64) {
				cand.Stop()
				continue
			}

			m.mu.Lock()
			if m.closed || !m.warmStandbyEnabled || m.active == nil || m.standby != nil {
				m.mu.Unlock()
				cand.Stop()
				return
			}
			m.standby = cand
			m.mu.Unlock()
			m.logger.Printf("[warp-fallback] warm standby READY ip=%s prefix=%s colo=%s ep=%s (%dms)",
				cand.Trace.IP, cand.Trace.Prefix64, cand.Trace.Colo, cand.Trace.Endpoint, cand.Trace.HandshakeMs)
			return
		}
	}()
}

func (m *WarpFallbackManager) doThroughTunnel(ctx context.Context, tun *WarpTunnelInstance, req *http.Request) (*http.Response, error) {
	if !tun.Acquire() {
		return nil, errTunnelRetiring
	}

	var (
		resp *http.Response
		err  error
	)
	if tun.tlsClient != nil {
		freq, fErr := toFHTTPRequest(ctx, req)
		if fErr != nil {
			tun.Release()
			return nil, fErr
		}
		fresp, doErr := tun.tlsClient.Do(freq)
		if doErr != nil {
			tun.Release()
			return nil, doErr
		}
		resp = toStandardResponse(fresp)
	} else if tun.stdClient != nil {
		resp, err = tun.stdClient.Do(req.WithContext(ctx))
		if err != nil {
			tun.Release()
			return nil, err
		}
	} else {
		tun.Release()
		return nil, fmt.Errorf("warp tunnel has no HTTP client configured")
	}

	resp.Body = &releaseBody{
		ReadCloser: resp.Body,
		onRelease:  tun.Release,
	}
	return resp, nil
}

func (m *WarpFallbackManager) markSuccess(tun *WarpTunnelInstance, latencyMs int64, cfg config.Config) {
	m.mu.Lock()
	m.lastUsedAt = time.Now()
	m.consecutive403 = 0
	firstForGen := m.loggedGen != tun.Generation
	if firstForGen {
		m.loggedGen = tun.Generation
	}
	m.mu.Unlock()

	m.store.RecordOK(
		tun.Trace.IP,
		tun.Trace.Prefix64,
		tun.Trace.Colo,
		tun.Trace.Endpoint,
		tun.Trace.HandshakeMs,
		latencyMs,
		firstForGen,
	)

	m.triggerStandbyWarmAsync(cfg)
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
			idleTimeout := m.idleTimeout
			m.mu.Unlock()
			if active == nil {
				continue
			}
			if idleTimeout <= 0 {
				idleTimeout = 30 * time.Minute
			}
			ref := lastUsed
			if ref.IsZero() {
				ref = active.ConnectedAt
			}
			if time.Since(ref) > idleTimeout {
				m.logger.Printf("[warp-fallback] tunnel idle for >%s; shutting down wireproxy to free memory", idleTimeout.Round(time.Second))
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
