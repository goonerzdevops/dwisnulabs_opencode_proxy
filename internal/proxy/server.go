package proxy

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"opencode-proxy/internal/config"
	"opencode-proxy/internal/web"
)

// Server exposes the OpenAI-compatible surface on top of the OpenCode free tier.
type Server struct {
	manager   *config.Manager
	upstream  *Upstream
	sessions  *SessionStore
	catalog   *ModelCatalog
	telemetry *TelemetryStore
	puter     *PuterService
	kilo      *KiloService
	bridge    *BrowserBridge
	logger    *log.Logger
	startedAt time.Time
	requests  atomic.Int64
}

// NewServer wires the proxy dependencies together.
func NewServer(manager *config.Manager, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	sessions := NewSessionStore(time.Duration(manager.Get().SessionTTLMinutes)*time.Minute, 1000)
	upstream := NewUpstream(manager.Get(), logger, sessions)
	baseDir := filepath.Dir(manager.Path())
	warpMgr := NewWarpFallbackManager(baseDir, logger)
	upstream.SetWarpManager(warpMgr)
	manager.Configure(func(cfg config.Config) {
		sessions.SetTTL(time.Duration(cfg.SessionTTLMinutes) * time.Minute)
		upstream.SyncConfig(cfg)
	})

	walPath := filepath.Join(baseDir, "logs", "usage.jsonl")
	telemetry := NewTelemetryStore(200, walPath)

	return &Server{
		manager:   manager,
		upstream:  upstream,
		sessions:  sessions,
		catalog:   NewModelCatalog(upstream),
		telemetry: telemetry,
		puter:     NewPuterService(logger),
		kilo:      NewKiloService(logger),
		bridge:    NewBrowserBridge(logger),
		logger:    logger,
		startedAt: time.Now(),
	}
}

// Handler returns the fully wired HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", s.guard(s.handleChatCompletions))
	mux.HandleFunc("POST /v1/messages", s.guard(s.handleAnthropicMessages))
	mux.HandleFunc("GET /v1/models", s.guard(s.handleModels))

	// Generic v2 endpoints
	mux.HandleFunc("POST /v2/chat/completions", s.guard(s.handleV2ChatCompletions))
	mux.HandleFunc("POST /v2/v1/chat/completions", s.guard(s.handleV2ChatCompletions))
	mux.HandleFunc("GET /v2/models", s.guard(s.handleV2Models))
	mux.HandleFunc("GET /v2/v1/models", s.guard(s.handleV2Models))

	// Dedicated Puter endpoints
	mux.HandleFunc("POST /v2/puter/chat/completions", s.guard(s.handlePuterChatCompletions))
	mux.HandleFunc("POST /v2/puter/v1/chat/completions", s.guard(s.handlePuterChatCompletions))
	mux.HandleFunc("GET /v2/puter/models", s.guard(s.handlePuterModels))
	mux.HandleFunc("GET /v2/puter/v1/models", s.guard(s.handlePuterModels))

	// Dedicated Kilo endpoints
	mux.HandleFunc("POST /v2/kilo/chat/completions", s.guard(s.handleKiloChatCompletions))
	mux.HandleFunc("POST /v2/kilo/v1/chat/completions", s.guard(s.handleKiloChatCompletions))
	mux.HandleFunc("GET /v2/kilo/models", s.guard(s.handleKiloModels))
	mux.HandleFunc("GET /v2/kilo/v1/models", s.guard(s.handleKiloModels))

	// Browser Relay Bridge endpoints
	mux.HandleFunc("GET /api/browser-relay/events", s.bridge.HandleEvents)
	mux.HandleFunc("POST /api/browser-relay/ready", s.bridge.HandleReady)
	mux.HandleFunc("POST /api/browser-relay/chunk", s.bridge.HandleChunk)
	mux.HandleFunc("POST /api/browser-relay/done", s.bridge.HandleDone)
	mux.HandleFunc("POST /api/browser-relay/error", s.bridge.HandleError)
	mux.HandleFunc("GET /api/browser-relay/status", s.bridge.HandleStatus)

	mux.HandleFunc("GET /api/config", s.guard(s.handleConfigGet))
	mux.HandleFunc("PUT /api/config", s.guard(s.handleConfigPut))
	mux.HandleFunc("GET /api/telemetry", s.guard(s.handleTelemetryGet))
	mux.HandleFunc("POST /api/telemetry/clear", s.guard(s.handleTelemetryClear))
	mux.HandleFunc("DELETE /api/telemetry", s.guard(s.handleTelemetryClear))
	mux.HandleFunc("GET /api/warp/status", s.guard(s.handleWarpStatus))
	mux.HandleFunc("POST /api/warp/hop", s.guard(s.handleWarpHop))
	mux.HandleFunc("POST /api/warp/stop", s.guard(s.handleWarpStop))
	mux.HandleFunc("POST /api/warp/burned/clear", s.guard(s.handleWarpBurnedClear))
	mux.HandleFunc("GET /health", s.handleHealth)
	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.Handle("GET /", web.Handler())
	return mux
}

// guard enforces the configured API key policy.
func (s *Server) guard(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := s.manager.Get()
		if !authorized(cfg, r) {
			writeError(w, http.StatusUnauthorized, "invalid_api_key",
				"Incorrect API key provided. Pass it as `Authorization: Bearer <key>`.")
			return
		}
		next(w, r)
	}
}

func authorized(cfg config.Config, r *http.Request) bool {
	if cfg.PermissiveAuth {
		return true
	}
	presented := presentedKey(r)
	if presented == "" {
		return false
	}
	for _, key := range cfg.APIKeys {
		if subtle.ConstantTimeCompare([]byte(key), []byte(presented)) == 1 {
			return true
		}
	}
	return false
}

func presentedKey(r *http.Request) string {
	if header := strings.TrimSpace(r.Header.Get("Authorization")); header != "" {
		if len(header) > 7 && strings.EqualFold(header[:7], "bearer ") {
			return strings.TrimSpace(header[7:])
		}
		return header
	}
	return strings.TrimSpace(r.Header.Get("x-api-key"))
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	cfg := s.manager.Get()
	body := map[string]any{
		"status":              "ok",
		"uptime_seconds":      int(time.Since(s.startedAt).Seconds()),
		"requests":            s.requests.Load(),
		"sessions":            s.sessions.Len(),
		"upstream":            cfg.UpstreamBaseURL,
		"default_model":       cfg.DefaultModel,
		"proxies":             s.upstream.Pool().Status(),
		"total_prompt_tokens": s.telemetry.TotalPromptTokens(),
		"total_cached_tokens": s.telemetry.TotalCachedTokens(),
		"total_output_tokens": s.telemetry.TotalOutputTokens(),
		"cache_hit_pct":       s.telemetry.OverallCacheHitPct(),
	}
	if wm := s.upstream.Warp(); wm != nil {
		body["warp"] = wm.Status(cfg, s.upstream.Pool())
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) handleTelemetryGet(w http.ResponseWriter, _ *http.Request) {
	summary := s.telemetry.Summary(50)
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleTelemetryClear(w http.ResponseWriter, _ *http.Request) {
	s.telemetry.Clear(true)
	writeJSON(w, http.StatusOK, map[string]any{
		"status":  "cleared",
		"message": "In-memory telemetry and logs have been reset.",
	})
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		writeError(w, http.StatusNotFound, "not_found", "Unknown path "+r.URL.Path)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("opencode-proxy: OpenAI-compatible gateway for the OpenCode free tier\n" +
		"endpoints: POST /v1/chat/completions, GET /v1/models, GET /health\n"))
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	cfg := s.manager.Get()
	force := r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true"
	entries := s.catalog.List(r.Context(), cfg, s.fingerprintFor(r, nil), force)
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": entries})
}

func (s *Server) handleV2Models(w http.ResponseWriter, r *http.Request) {
	cfg := s.manager.Get()
	force := r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true"

	var puterModels []ModelEntry
	if cfg.PuterEnabled {
		puterModels = s.puter.FetchModels(r.Context(), force)
	}

	var kiloModels []ModelEntry
	if cfg.KiloEnabled {
		kiloModels = s.kilo.FetchModels(r.Context(), force)
	}

	// Exclude opencode models from /v2/models so client dropdowns are clean & curated.
	combined := make([]ModelEntry, 0, len(puterModels)+len(kiloModels))
	combined = append(combined, puterModels...)
	combined = append(combined, kiloModels...)

	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": combined})
}

func (s *Server) handleKiloModels(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true"
	kiloModels := s.kilo.FetchModels(r.Context(), force)
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": kiloModels})
}

func (s *Server) handleKiloChatCompletions(w http.ResponseWriter, r *http.Request) {
	body, err := decodeJSONBody(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	s.forwardKiloChat(w, r, body)
}

func (s *Server) handlePuterModels(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true"
	puterModels := s.puter.FetchModels(r.Context(), force)
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": puterModels})
}

func (s *Server) handleConfigGet(w http.ResponseWriter, _ *http.Request) {
	cfg := s.manager.Get()
	body := map[string]any{
		"config":    cfg,
		"path":      s.manager.Path(),
		"read_only": false,
	}
	writeJSON(w, http.StatusOK, body)
}

func (s *Server) handleConfigPut(w http.ResponseWriter, r *http.Request) {
	var next config.Config
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := decoder.Decode(&next); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_config", err.Error())
		return
	}
	if err := s.manager.Update(next); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_config", err.Error())
		return
	}
	s.logger.Printf("configuration updated (path=%s)", s.manager.Path())
	writeJSON(w, http.StatusOK, map[string]any{"status": "updated", "config": s.manager.Get()})
}

func (s *Server) handleWarpStatus(w http.ResponseWriter, _ *http.Request) {
	cfg := s.manager.Get()
	wm := s.upstream.Warp()
	if wm == nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "active_route": "direct"})
		return
	}
	writeJSON(w, http.StatusOK, wm.Status(cfg, s.upstream.Pool()))
}

func (s *Server) handleWarpHop(w http.ResponseWriter, r *http.Request) {
	cfg := s.manager.Get()
	wm := s.upstream.Warp()
	if wm == nil {
		writeError(w, http.StatusServiceUnavailable, "warp_unavailable", "WARP fallback manager is not initialized")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	trace, err := wm.ManualHop(ctx, cfg)
	if err != nil {
		writeError(w, http.StatusBadGateway, "warp_hop_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "hopped",
		"trace":       trace,
		"warp_status": wm.Status(cfg, s.upstream.Pool()),
	})
}

func (s *Server) handleWarpStop(w http.ResponseWriter, _ *http.Request) {
	cfg := s.manager.Get()
	if wm := s.upstream.Warp(); wm != nil {
		wm.Stop("manual_stop")
	}
	if pool := s.upstream.Pool(); pool != nil {
		pool.ClearCooldown()
	}
	var st map[string]any
	if wm := s.upstream.Warp(); wm != nil {
		st = wm.Status(cfg, s.upstream.Pool())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "stopped",
		"message":     "WARP tunnel stopped and primary direct egress cooldown cleared.",
		"warp_status": st,
	})
}

func (s *Server) handleWarpBurnedClear(w http.ResponseWriter, _ *http.Request) {
	cfg := s.manager.Get()
	if wm := s.upstream.Warp(); wm != nil {
		wm.Store().ClearBurned()
	}
	var st map[string]any
	if wm := s.upstream.Warp(); wm != nil {
		st = wm.Status(cfg, s.upstream.Pool())
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      "cleared",
		"message":     "Active WARP burned /64 list cleared.",
		"warp_status": st,
	})
}

// Close gracefully releases background workers such as telemetry WAL writer and WARP wireproxy.
func (s *Server) Close() {
	if s.upstream != nil {
		s.upstream.Close()
	}
	if s.telemetry != nil {
		s.telemetry.Close()
	}
}
