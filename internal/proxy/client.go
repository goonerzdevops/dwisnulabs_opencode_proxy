package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"opencode-proxy/internal/config"
)

// Upstream posts fingerprint-shaped requests to the OpenCode free tier.
type Upstream struct {
	pool *ProxyPool
}

// NewUpstream builds an Upstream client backed by an outbound ProxyPool.
func NewUpstream(cfg config.Config, logger *log.Logger) *Upstream {
	return &Upstream{
		pool: NewProxyPool(cfg, logger),
	}
}

// SyncConfig syncs the proxy pool with the new configuration.
func (u *Upstream) SyncConfig(cfg config.Config) {
	if u.pool != nil {
		u.pool.Sync(cfg)
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

// Send posts the prepared body and returns the raw upstream response. The caller
// owns the response body. If a proxy node hits 429 or 403, it automatically fails over.
func (u *Upstream) Send(ctx context.Context, cfg config.Config, prepared *PreparedRequest, fp Fingerprint) (*http.Response, error) {
	payload, err := json.Marshal(prepared.Body)
	if err != nil {
		return nil, fmt.Errorf("encode upstream body: %w", err)
	}

	url := strings.TrimRight(cfg.UpstreamBaseURL, "/") + prepared.Path
	reqFactory := func() (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
		if err != nil {
			return nil, err
		}
		u.applyHeaders(req.Header, true, fp)
		return req, nil
	}

	response, _, err := u.pool.DoWithFailover(ctx, cfg, reqFactory)
	if err != nil {
		return nil, fmt.Errorf("upstream request failed: %w", err)
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
