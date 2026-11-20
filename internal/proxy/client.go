package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"opencode-proxy/internal/config"
)

// Upstream posts fingerprint-shaped requests to the OpenCode free tier.
type Upstream struct {
	pool           *ProxyPool
	logger         *log.Logger
	sessionStore   *SessionStore
	warp           *WarpFallbackManager
	concealMu      sync.RWMutex
	concealLearned map[string]time.Time
}

// NewUpstream builds an Upstream client backed by an outbound ProxyPool.
// Optionally accepts a SessionStore for automatic rejection invalidation.
func NewUpstream(cfg config.Config, logger *log.Logger, sessionStore ...*SessionStore) *Upstream {
	var store *SessionStore
	if len(sessionStore) > 0 {
		store = sessionStore[0]
	}
	return &Upstream{
		pool:           NewProxyPool(cfg, logger),
		logger:         logger,
		sessionStore:   store,
		concealLearned: make(map[string]time.Time),
	}
}

func (u *Upstream) isConcealLearned(sig string) bool {
	if sig == "" {
		return false
	}
	u.concealMu.RLock()
	exp, ok := u.concealLearned[sig]
	u.concealMu.RUnlock()
	return ok && time.Now().Before(exp)
}

func (u *Upstream) learnConceal(sig string) {
	if sig == "" {
		return
	}
	u.concealMu.Lock()
	defer u.concealMu.Unlock()
	if u.concealLearned == nil {
		u.concealLearned = make(map[string]time.Time)
	}
	if len(u.concealLearned) >= 128 {
		now := time.Now()
		for k, exp := range u.concealLearned {
			if now.After(exp) {
				delete(u.concealLearned, k)
			}
		}
		if len(u.concealLearned) >= 128 {
			u.concealLearned = make(map[string]time.Time)
		}
	}
	u.concealLearned[sig] = time.Now().Add(6 * time.Hour)
}

// SetSessionStore assigns the session store to enable automatic session invalidation.
func (u *Upstream) SetSessionStore(store *SessionStore) {
	u.sessionStore = store
}

// SetWarpManager attaches the lazy WARP IPv6 fallback manager.
func (u *Upstream) SetWarpManager(warp *WarpFallbackManager) {
	u.warp = warp
}

// Warp returns the attached WARP IPv6 fallback manager (or nil if unconfigured).
func (u *Upstream) Warp() *WarpFallbackManager {
	return u.warp
}

// Close releases background WARP tunnel processes if active.
func (u *Upstream) Close() {
	if u.warp != nil {
		u.warp.Close()
	}
}

// SyncConfig syncs the proxy pool and WARP manager with the new configuration.
func (u *Upstream) SyncConfig(cfg config.Config) {
	if u.pool != nil {
		u.pool.Sync(cfg)
	}
	if u.warp != nil {
		u.warp.SyncConfig(cfg)
	}
}

// Pool returns the underlying ProxyPool.
func (u *Upstream) Pool() *ProxyPool {
	return u.pool
}

// Fingerprint identifies the downstream caller for session continuity.
type Fingerprint struct {
	SessionID string
	Project   string
}

// applyHeaders writes the impersonation headers the free-tier gate validates.
// Missing x-opencode-client/User-Agent values are answered with 403
// FreeTierError, and an unstable session id triggers 429 FreeUsageLimitError.
func (u *Upstream) applyHeaders(header http.Header, stream bool, fp Fingerprint) {
	session := fp.SessionID
	if session == "" {
		session = TranslateSessionID("", "")
	}
	project := fp.Project
	if project == "" {
		project = UpstreamProject
	}

	header.Set("Content-Type", "application/json")
	header.Set("Authorization", UpstreamAuth)
	header.Set("User-Agent", UpstreamUserAgent)
	header.Set("x-opencode-client", UpstreamClient)
	header.Set("x-opencode-project", project)
	header.Set("x-opencode-session", session)
	header.Set("x-opencode-session-id", session)
	header.Set("x-session-affinity", session)
	header.Set("x-session-id", session)

	traceID := randomHex(16)
	spanID := randomHex(8)
	parentSpanID := randomHex(8)
	header.Set("b3", fmt.Sprintf("%s-%s-1-%s", traceID, spanID, parentSpanID))
	header.Set("traceparent", fmt.Sprintf("00-%s-%s-01", traceID, spanID))
	header.Set("x-opencode-request", "msg_"+randomBase62(24))
	if stream {
		header.Set("Accept", "text/event-stream")
	} else {
		header.Set("Accept", "application/json")
	}
}

// isOverload429 checks if an upstream 429 response indicates transient server overload.
func isOverload429(body []byte) bool {
	lower := strings.ToLower(string(body))
	return strings.Contains(lower, "overloaded")
}

// Send posts the prepared body and returns the raw upstream response. The caller
// owns the response body. If upstream returns a transient overload 429 ("The inference
// service is overloaded"), it automatically retries with exponential backoff (up to 5 retries).
// If it returns 403 (FreeTierError) or non-overload 429 (quota limit), the rejected session is dropped.
func (u *Upstream) Send(ctx context.Context, cfg config.Config, prepared *PreparedRequest, fp Fingerprint) (*http.Response, error) {
	// Proactively conceal caller toolsets that previously triggered content-based 429s
	// so subsequent turns skip the wasted 429 round-trip entirely.
	toolSig := CallerToolsSignature(prepared)
	if toolSig != "" && u.isConcealLearned(toolSig) {
		ConcealAllCallerTools(prepared)
	}

	payload, err := json.Marshal(prepared.Body)
	if err != nil {
		return nil, fmt.Errorf("encode upstream body: %w", err)
	}

	url := strings.TrimRight(cfg.UpstreamBaseURL, "/") + prepared.Path
	reqFactoryWithFP := func(curFP Fingerprint) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		u.applyHeaders(req.Header, true, curFP)
		return req, nil
	}
	reqFactory := func() (*http.Request, error) {
		return reqFactoryWithFP(fp)
	}

	delays := []time.Duration{
		800 * time.Millisecond,
		1400 * time.Millisecond,
		2200 * time.Millisecond,
		3200 * time.Millisecond,
		4500 * time.Millisecond,
	}

	var response *http.Response
	forceDirect := false // set after a direct tool-conceal retry: stay on the primary egress
	didConcealRetry := false
	for attempt := 0; attempt <= len(delays); attempt++ {
		var (
			resp *http.Response
			err  error
		)
		unconcealedTools := HasUnconcealedCallerTools(prepared)
		usingWarpDirect := !forceDirect && u.warp != nil && u.warp.ShouldUseFallback(cfg, u.pool)
		if usingWarpDirect {
			resp, err = u.warp.SendWithOptions(ctx, cfg, reqFactoryWithFP, fp, u.sessionStore, WarpSendOptions{
				ReturnOnFirst429WithoutBurn: unconcealedTools,
			})
		} else {
			resp, _, err = u.pool.DoWithFailoverMode(ctx, cfg, reqFactory, unconcealedTools)
		}
		if err != nil {
			return nil, fmt.Errorf("upstream request failed: %w", err)
		}
		response = resp

		if resp.StatusCode == http.StatusOK {
			if didConcealRetry && toolSig != "" {
				u.learnConceal(toolSig)
			}
			if forceDirect {
				// The earlier 429 was caused by tool content, not egress quota.
				u.pool.ClearCooldown()
			}
			return response, nil
		}

		// Snapshot error body for classification (up to 4KB)
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		_ = resp.Body.Close()

		// 1. Transient overload 429 retry
		if resp.StatusCode == http.StatusTooManyRequests && isOverload429(bodyBytes) {
			if attempt < len(delays) {
				backoff := delays[attempt] + time.Duration(rand.Intn(250))*time.Millisecond
				if u.logger != nil {
					u.logger.Printf("[overload-retry] upstream inference overloaded (attempt %d/%d), retrying in %v...", attempt+1, len(delays), backoff)
				}
				select {
				case <-time.After(backoff):
					continue
				case <-ctx.Done():
					return nil, ctx.Err()
				}
			}
			if u.logger != nil {
				u.logger.Printf("[overload-retry] upstream inference still overloaded after %d retry attempts", len(delays))
			}
			response.Body = io.NopCloser(bytes.NewReader(bodyBytes))
			return response, nil
		}

		// 1b. Upstream also answers 429 FreeUsageLimitError for some third-party tool
		// descriptions. Conceal caller tools once on the SAME egress (direct stays direct;
		// WARP stays on the exact same WARP tunnel) before burning IPv6 or cooling down direct.
		if resp.StatusCode == http.StatusTooManyRequests && ConcealAllCallerTools(prepared) {
			if newPayload, mErr := json.Marshal(prepared.Body); mErr == nil {
				payload = newPayload
				didConcealRetry = true
				if fp.SessionID != "" {
					InvalidateSession(fp.SessionID, u.sessionStore)
				}
				fp = Fingerprint{SessionID: GenerateSessionID(time.Now().UnixMilli()), Project: fp.Project}
				if u.logger != nil {
					egress := "direct"
					if usingWarpDirect {
						egress = "warp_ipv6"
					}
					u.logger.Printf("[tool-conceal] upstream 429 with caller tools on %s; retrying once with concealed tools on same egress", egress)
				}
				if usingWarpDirect && u.warp != nil {
					u.warp.Store().RecordEvent(WarpLogEntry{
						Event:  "tool_conceal",
						Reason: "concealed caller tools before burning WARP IPv6",
					})
				}
				attempt--
				if !usingWarpDirect {
					forceDirect = true
				}
				continue
			}
		}

		// 2. Spent/rejected session drop (HTTP 403 FreeTierError or quota 429 FreeUsageLimitError)
		if resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests {
			if fp.SessionID != "" {
				InvalidateSession(fp.SessionID, u.sessionStore)
				if u.logger != nil {
					u.logger.Printf("[session-drop] upstream rejected session %s (HTTP %d), invalidated from cache for future requests", fp.SessionID, resp.StatusCode)
				}
			}

			// 3. Lazy WARP IPv6 Fallback when primary egress hits quota limit
			if !usingWarpDirect && cfg.WarpFallbackEnabled && u.warp != nil {
				freshFP := Fingerprint{
					SessionID: GenerateSessionID(time.Now().UnixMilli()),
					Project:   fp.Project,
				}
				if u.logger != nil {
					u.logger.Printf("[warp-fallback] primary egress hit HTTP %d; failing over transparently to WARP IPv6 (new session=%s)",
						resp.StatusCode, freshFP.SessionID)
				}
				warpResp, warpErr := u.warp.Send(ctx, cfg, reqFactoryWithFP, freshFP, u.sessionStore)
				if warpErr == nil && warpResp != nil {
					return warpResp, nil
				}
				if u.logger != nil && warpErr != nil {
					u.logger.Printf("[warp-fallback] fallback attempt failed: %v", warpErr)
				}
			}
		}

		// Non-retryable error: restore response body and return
		response.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		return response, nil
	}

	return response, nil
}

// FetchModels lists provider models. The free tier exposes its catalogue on the
// inference prefix; older /zen/v1 deployments answer the same document.
func (u *Upstream) FetchModels(ctx context.Context, cfg config.Config, fp Fingerprint) ([]string, error) {
	base := strings.TrimRight(cfg.UpstreamBaseURL, "/")
	var lastErr error

	for _, path := range []string{"/zen/v1/models", "/inference/openai/v1/models", "/inference/v1/models"} {
		url := base + path
		reqFactory := func() (*http.Request, error) {
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
			if err != nil {
				return nil, err
			}
			u.applyHeaders(req.Header, false, fp)
			return req, nil
		}

		response, _, err := u.pool.DoWithFailover(ctx, cfg, reqFactory)
		if err != nil {
			lastErr = err
			continue
		}
		blob, readErr := io.ReadAll(io.LimitReader(response.Body, 4<<20))
		response.Body.Close()
		if readErr != nil {
			lastErr = readErr
			continue
		}
		if response.StatusCode != http.StatusOK {
			if response.StatusCode == http.StatusForbidden || response.StatusCode == http.StatusTooManyRequests {
				if fp.SessionID != "" {
					InvalidateSession(fp.SessionID, u.sessionStore)
				}
			}
			lastErr = fmt.Errorf("%s returned %d: %s", path, response.StatusCode, truncate(string(blob), 200))
			continue
		}
		ids, parseErr := parseModelList(blob)
		if parseErr != nil {
			lastErr = parseErr
			continue
		}
		if len(ids) > 0 {
			return ids, nil
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no model catalogue available")
	}
	return nil, lastErr
}

func parseModelList(blob []byte) ([]string, error) {
	var envelope struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(blob, &envelope); err == nil {
		ids := make([]string, 0, len(envelope.Data))
		for _, model := range envelope.Data {
			if id := strings.TrimSpace(model.ID); id != "" {
				ids = append(ids, id)
			}
		}
		if len(ids) > 0 {
			return ids, nil
		}
	}

	var list []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(blob, &list); err != nil {
		return nil, fmt.Errorf("decode model catalogue: %w", err)
	}
	ids := make([]string, 0, len(list))
	for _, model := range list {
		if id := strings.TrimSpace(model.ID); id != "" {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func truncate(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit] + "..."
}
