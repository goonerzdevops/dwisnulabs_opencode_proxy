package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	fhttp "github.com/bogdanfinn/fhttp"
	tls_client "github.com/bogdanfinn/tls-client"
	"github.com/bogdanfinn/tls-client/profiles"
	utls "github.com/bogdanfinn/utls"

	"opencode-proxy/internal/config"
)

// ProxyNode represents a single outbound egress route (direct or proxy).
type ProxyNode struct {
	URL           string    `json:"url"`
	Healthy       bool      `json:"healthy"`
	CooldownUntil time.Time `json:"cooldown_until"`
	client        *http.Client
	tlsClient     tls_client.HttpClient
	sem           chan struct{}
	lastReqMu     sync.Mutex
	lastReqAt     time.Time
}

type releaseBody struct {
	io.ReadCloser
	onRelease func()
	once      sync.Once
}

func (r *releaseBody) release() {
	r.once.Do(func() {
		if r.onRelease != nil {
			r.onRelease()
		}
	})
}

func (r *releaseBody) Read(p []byte) (n int, err error) {
	n, err = r.ReadCloser.Read(p)
	if err != nil {
		r.release()
	}
	return n, err
}

func (r *releaseBody) Close() error {
	r.release()
	return r.ReadCloser.Close()
}

func (n *ProxyNode) acquire(ctx context.Context, cfg config.Config) error {
	if cfg.AntiAbuseEnabled && n.sem != nil {
		select {
		case n.sem <- struct{}{}:
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if cfg.AntiAbuseEnabled {
		n.lastReqMu.Lock()
		minInterval := time.Duration(cfg.MinRequestIntervalMs) * time.Millisecond
		if minInterval <= 0 {
			minInterval = 300 * time.Millisecond
		}
		jitter := time.Duration(rand.Intn(50)) * time.Millisecond
		targetDelay := minInterval + jitter

		elapsed := time.Since(n.lastReqAt)
		if !n.lastReqAt.IsZero() && elapsed < targetDelay {
			sleepDur := targetDelay - elapsed
			n.lastReqMu.Unlock()
			select {
			case <-time.After(sleepDur):
			case <-ctx.Done():
				n.release(cfg)
				return ctx.Err()
			}
		} else {
			n.lastReqMu.Unlock()
		}
	}
	return nil
}

func (n *ProxyNode) release(cfg config.Config) {
	n.lastReqMu.Lock()
	n.lastReqAt = time.Now()
	n.lastReqMu.Unlock()

	if cfg.AntiAbuseEnabled && n.sem != nil {
		select {
		case <-n.sem:
		default:
		}
	}
}

// ProxyPool manages multiple outbound proxy nodes and handles transparent failover.
type ProxyPool struct {
	mu     sync.RWMutex
	nodes  []*ProxyNode
	index  int
	logger *log.Logger
}

// NewProxyPool builds a ProxyPool from the configured proxy URLs.
func NewProxyPool(cfg config.Config, logger *log.Logger) *ProxyPool {
	if logger == nil {
		logger = log.Default()
	}
	pool := &ProxyPool{
		logger: logger,
	}
	pool.Sync(cfg)
	return pool
}

// Sync updates the pool nodes to match the new configuration.
func (p *ProxyPool) Sync(cfg config.Config) {
	p.mu.Lock()
	defer p.mu.Unlock()

	proxies := cfg.Proxies
	if len(proxies) == 0 {
		proxies = []string{"direct"}
	}

	maxConcurrent := cfg.MaxConcurrentPerNode
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}

	newNodes := make([]*ProxyNode, 0, len(proxies))
	for _, pURL := range proxies {
		pURL = strings.TrimSpace(pURL)
		if pURL == "" {
			continue
		}

		client := createClientForProxy(pURL)
		tlsClient := createTLSClientForProxy(pURL, cfg.UpstreamTimeoutSeconds)
		newNodes = append(newNodes, &ProxyNode{
			URL:           pURL,
			Healthy:       true,
			CooldownUntil: time.Time{},
			client:        client,
			tlsClient:     tlsClient,
			sem:           make(chan struct{}, maxConcurrent),
		})
	}

	if len(newNodes) == 0 {
		newNodes = append(newNodes, &ProxyNode{
			URL:           "direct",
			Healthy:       true,
			client:        createClientForProxy("direct"),
			tlsClient:     createTLSClientForProxy("direct", cfg.UpstreamTimeoutSeconds),
			sem:           make(chan struct{}, maxConcurrent),
		})
	}

	p.nodes = newNodes
	p.index = 0
}

// openCodeCipherSuites matches the exact BoringSSL/Chrome cipher list required by opencode.ai's
// free-tier TLS gate (JA4 hash 5b57614c22b0). Requests without this exact cipher list are rejected
// with 429 FreeUsageLimitError.
var openCodeCipherSuites = []uint16{
	utls.TLS_AES_128_GCM_SHA256,
	utls.TLS_AES_256_GCM_SHA384,
	utls.TLS_CHACHA20_POLY1305_SHA256,
	utls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	utls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	utls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	utls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	utls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	utls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
	utls.TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA,
	utls.TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA,
	utls.TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA,
	utls.TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA,
	utls.TLS_RSA_WITH_AES_128_GCM_SHA256,
	utls.TLS_RSA_WITH_AES_256_GCM_SHA384,
	utls.TLS_RSA_WITH_AES_128_CBC_SHA,
	utls.TLS_RSA_WITH_AES_256_CBC_SHA,
}

func getOpenCodeProfile() profiles.ClientProfile {
	base := profiles.Chrome_120
	baseSpec, _ := base.GetClientHelloSpec()
	customID := utls.ClientHelloID{
		Client: "OpenCode-BoringSSL",
		SpecFactory: func() (utls.ClientHelloSpec, error) {
			spec := baseSpec
			spec.CipherSuites = openCodeCipherSuites
			return spec, nil
		},
	}
	return profiles.NewClientProfile(
		customID,
		base.GetSettings(),
		base.GetSettingsOrder(),
		base.GetPseudoHeaderOrder(),
		base.GetConnectionFlow(),
		base.GetPriorities(),
		base.GetHeaderPriority(),
		base.GetStreamID(),
		false,
		base.GetHttp3Settings(),
		base.GetHttp3SettingsOrder(),
		base.GetHttp3PriorityParam(),
		base.GetHttp3PseudoHeaderOrder(),
		false,
	)
}

func createTLSClientForProxy(proxyStr string, timeoutSec int) tls_client.HttpClient {
	if timeoutSec <= 0 {
		timeoutSec = 120
	}
	options := []tls_client.HttpClientOption{
		tls_client.WithTimeoutSeconds(timeoutSec),
		tls_client.WithClientProfile(getOpenCodeProfile()),
		tls_client.WithNotFollowRedirects(),
	}

	if !strings.EqualFold(proxyStr, "direct") && proxyStr != "" {
		options = append(options, tls_client.WithProxyUrl(proxyStr))
	}

	client, err := tls_client.NewHttpClient(tls_client.NewNoopLogger(), options...)
	if err != nil {
		return nil
	}
	return client
}

func toFHTTPRequest(ctx context.Context, stdReq *http.Request) (*fhttp.Request, error) {
	var bodyReader io.Reader
	if stdReq.Body != nil {
		bodyBytes, err := io.ReadAll(stdReq.Body)
		if err != nil {
			return nil, err
		}
		stdReq.Body.Close()
		stdReq.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		bodyReader = bytes.NewReader(bodyBytes)
	}

	freq, err := fhttp.NewRequestWithContext(ctx, stdReq.Method, stdReq.URL.String(), bodyReader)
	if err != nil {
		return nil, err
	}

	for k, vv := range stdReq.Header {
		for _, v := range vv {
			freq.Header.Add(k, v)
		}
	}
	return freq, nil
}

func toStandardResponse(fresp *fhttp.Response) *http.Response {
	stdHeader := make(http.Header, len(fresp.Header))
	for k, v := range fresp.Header {
		stdHeader[k] = v
	}
	return &http.Response{
		Status:           fresp.Status,
		StatusCode:       fresp.StatusCode,
		Proto:            fresp.Proto,
		ProtoMajor:       fresp.ProtoMajor,
		ProtoMinor:       fresp.ProtoMinor,
		Header:           stdHeader,
		Body:             fresp.Body,
		ContentLength:    fresp.ContentLength,
		TransferEncoding: fresp.TransferEncoding,
		Close:            fresp.Close,
		Uncompressed:     fresp.Uncompressed,
		Trailer:          http.Header(fresp.Trailer),
	}
}

func createClientForProxy(proxyStr string) *http.Client {
	transport := &http.Transport{
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   32,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
	}

	if strings.EqualFold(proxyStr, "direct") {
		transport.Proxy = http.ProxyFromEnvironment
	} else {
		if parsed, err := url.Parse(proxyStr); err == nil {
			transport.Proxy = http.ProxyURL(parsed)
		} else {
			transport.Proxy = http.ProxyFromEnvironment
		}
	}

	return &http.Client{Transport: transport}
}

// NextCandidate returns the best available proxy node, skipping cooled down nodes if possible.
func (p *ProxyPool) NextCandidate() (*ProxyNode, int) {
	p.mu.Lock()
	defer p.mu.Unlock()

	n := len(p.nodes)
	if n == 0 {
		return nil, -1
	}

	now := time.Now()
	// Try to find a healthy node starting from current index
	for i := 0; i < n; i++ {
		idx := (p.index + i) % n
		node := p.nodes[idx]
		if now.After(node.CooldownUntil) {
			p.index = (idx + 1) % n
			node.Healthy = true
			return node, idx
		}
	}

	// If all are cooling down, return the one that expires soonest
	earliestIdx := 0
	earliestTime := p.nodes[0].CooldownUntil
	for i := 1; i < n; i++ {
		if p.nodes[i].CooldownUntil.Before(earliestTime) {
			earliestIdx = i
			earliestTime = p.nodes[i].CooldownUntil
		}
	}
	p.index = (earliestIdx + 1) % n
	return p.nodes[earliestIdx], earliestIdx
}

// MarkCooldown puts a node into cooldown after a 429 or 403 rate-limit response.
func (p *ProxyPool) MarkCooldown(idx int, duration time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if idx >= 0 && idx < len(p.nodes) {
		p.nodes[idx].Healthy = false
		p.nodes[idx].CooldownUntil = time.Now().Add(duration)
		p.logger.Printf("proxy node [%s] entered cooldown for %s", p.nodes[idx].URL, duration)
	}
}

// Status returns a snapshot of proxy nodes for monitoring and Web UI.
func (p *ProxyPool) Status() []map[string]any {
	p.mu.RLock()
	defer p.mu.RUnlock()

	now := time.Now()
	res := make([]map[string]any, 0, len(p.nodes))
	for _, n := range p.nodes {
		healthy := now.After(n.CooldownUntil)
		item := map[string]any{
			"url":     n.URL,
			"healthy": healthy,
		}
		if !healthy {
			item["cooldown_seconds_left"] = int(n.CooldownUntil.Sub(now).Seconds())
		}
		res = append(res, item)
	}
	return res
}

// DoWithFailover executes an HTTP request, automatically failing over to the next node if a 429 or 403 occurs.
func (p *ProxyPool) DoWithFailover(
	ctx context.Context,
	cfg config.Config,
	reqFactory func() (*http.Request, error),
) (*http.Response, string, error) {
	p.mu.RLock()
	totalNodes := len(p.nodes)
	p.mu.RUnlock()

	cooldown := time.Duration(cfg.ProxyCooldownSeconds) * time.Second
	if cooldown <= 0 {
		cooldown = 15 * time.Minute
	}

	maxAttempts := totalNodes
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastResp *http.Response
	var lastErr error

	for attempt := 0; attempt < maxAttempts; attempt++ {
		node, idx := p.NextCandidate()
		if node == nil {
			break
		}

		if err := node.acquire(ctx, cfg); err != nil {
			lastErr = err
			break
		}

		req, err := reqFactory()
		if err != nil {
			node.release(cfg)
			return nil, "", fmt.Errorf("build request: %w", err)
		}

		var resp *http.Response
		if node.tlsClient != nil {
			freq, fErr := toFHTTPRequest(ctx, req)
			if fErr != nil {
				node.release(cfg)
				return nil, "", fmt.Errorf("convert request to tls-client: %w", fErr)
			}
			fresp, doErr := node.tlsClient.Do(freq)
			if doErr != nil {
				node.release(cfg)
				lastErr = doErr
				p.logger.Printf("request via [%s] tls-client network error: %v, attempting next node", node.URL, doErr)
				continue
			}
			resp = toStandardResponse(fresp)
		} else {
			resp, err = node.client.Do(req.WithContext(ctx))
			if err != nil {
				node.release(cfg)
				lastErr = err
				p.logger.Printf("request via [%s] network error: %v, attempting next node", node.URL, err)
				continue
			}
		}

		// If 429 (rate limited) or 403 FreeTierError, mark cooldown
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusForbidden {
			p.MarkCooldown(idx, cooldown)
			if totalNodes > 1 {
				node.release(cfg)
				if lastResp != nil {
					io.Copy(io.Discard, io.LimitReader(lastResp.Body, 4096))
					lastResp.Body.Close()
				}
				lastResp = resp
				p.logger.Printf("request via [%s] returned HTTP %d, failing over to next proxy node", node.URL, resp.StatusCode)
				continue
			}
		}

		if lastResp != nil {
			io.Copy(io.Discard, io.LimitReader(lastResp.Body, 4096))
			lastResp.Body.Close()
		}

		resp.Body = &releaseBody{
			ReadCloser: resp.Body,
			onRelease: func() {
				node.release(cfg)
			},
		}

		return resp, node.URL, nil
	}

	if lastResp != nil {
		return lastResp, "last_attempted", nil
	}
	return nil, "", lastErr
}
