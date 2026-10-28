package proxy

import (
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
	logger    *log.Logger
	startedAt time.Time
	requests  atomic.Int64
}

// NewServer wires the proxy dependencies together.
func NewServer(manager *config.Manager, logger *log.Logger) *Server {
	if logger == nil {
		logger = log.Default()
	}
	upstream := NewUpstream(manager.Get(), logger)
	sessions := NewSessionStore(time.Duration(manager.Get().SessionTTLMinutes)*time.Minute, 1000)
	manager.Configure(func(cfg config.Config) {
		sessions.SetTTL(time.Duration(cfg.SessionTTLMinutes) * time.Minute)
		upstream.SyncConfig(cfg)
	})

	walPath := filepath.Join(filepath.Dir(manager.Path()), "logs", "usage.jsonl")
	telemetry := NewTelemetryStore(200, walPath)

	return &Server{
		manager:   manager,
		upstream:  upstream,
		sessions:  sessions,
		catalog:   NewModelCatalog(upstream),
		telemetry: telemetry,
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
	mux.HandleFunc("GET /api/config", s.guard(s.handleConfigGet))
	mux.HandleFunc("PUT /api/config", s.guard(s.handleConfigPut))
	mux.HandleFunc("GET /api/telemetry", s.guard(s.handleTelemetryGet))
	mux.HandleFunc("POST /api/telemetry/clear", s.guard(s.handleTelemetryClear))
	mux.HandleFunc("DELETE /api/telemetry", s.guard(s.handleTelemetryClear))
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

// Close gracefully releases background workers such as telemetry WAL writer.
func (s *Server) Close() {
	if s.telemetry != nil {
		s.telemetry.Close()
	}
}
