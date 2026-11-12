package proxy

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
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
		puter:     NewPuterService(logger),
		kilo:      NewKiloService(logger),
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

	// Dedicated Puter endpoints
	mux.HandleFunc("POST /v2/puter/chat/completions", s.guard(s.handlePuterChatCompletions))
	mux.HandleFunc("POST /v2/puter/v1/chat/completions", s.guard(s.handlePuterChatCompletions))
	mux.HandleFunc("GET /v2/puter/models", s.guard(s.handlePuterModels))
	mux.HandleFunc("POST /api/puter/ping", s.guard(s.handlePuterPing))
	mux.HandleFunc("GET /api/puter/accounts", s.guard(s.handlePuterAccountsGet))
	mux.HandleFunc("POST /api/puter/accounts", s.guard(s.handlePuterAccountsPost))
	mux.HandleFunc("DELETE /api/puter/accounts", s.guard(s.handlePuterAccountsDelete))
	mux.HandleFunc("POST /api/puter/accounts/reset", s.guard(s.handlePuterAccountsReset))
	mux.HandleFunc("POST /api/puter/accounts/priority", s.guard(s.handlePuterAccountsPriority))

	// Dedicated Kilo endpoints
	mux.HandleFunc("POST /v2/kilo/chat/completions", s.guard(s.handleKiloChatCompletions))
	mux.HandleFunc("POST /v2/kilo/v1/chat/completions", s.guard(s.handleKiloChatCompletions))
	mux.HandleFunc("GET /v2/kilo/models", s.guard(s.handleKiloModels))

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

func (s *Server) handleV2Models(w http.ResponseWriter, r *http.Request) {
	cfg := s.manager.Get()
	force := r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true"
	opencodeModels := s.catalog.List(r.Context(), cfg, s.fingerprintFor(r, nil), force)

	var puterModels []ModelEntry
	if cfg.PuterEnabled {
		puterModels = s.puter.FetchModels(r.Context(), cfg.PuterAPIKey, force)
	}

	var kiloModels []ModelEntry
	if cfg.KiloEnabled {
		kiloModels = s.kilo.FetchModels(r.Context(), force)
	}

	combined := make([]ModelEntry, 0, len(opencodeModels)+len(puterModels)+len(kiloModels))
	combined = append(combined, opencodeModels...)
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
	cfg := s.manager.Get()
	force := r.URL.Query().Get("refresh") == "1" || r.URL.Query().Get("refresh") == "true"
	puterModels := s.puter.FetchModels(r.Context(), cfg.PuterAPIKey, force)
	writeJSON(w, http.StatusOK, map[string]any{"object": "list", "data": puterModels})
}

func (s *Server) handlePuterPing(w http.ResponseWriter, r *http.Request) {
	cfg := s.manager.Get()
	var payload struct {
		Token     string `json:"token"`
		Model     string `json:"model"`
		AccountID string `json:"account_id"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<18)).Decode(&payload)
	}

	token := strings.TrimSpace(payload.Token)
	if payload.AccountID != "" {
		for _, acc := range s.manager.GetPuterAccounts() {
			if acc.ID == payload.AccountID {
				token = acc.Token
				break
			}
		}
	}
	if token == "" {
		token = strings.TrimSpace(cfg.PuterAPIKey)
	}
	if token == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"status":  "error",
			"message": "Token Puter masih kosong. Masukkan token terlebih dahulu.",
		})
		return
	}

	model := payload.Model
	if model == "" {
		model = cfg.PuterDefaultModel
	}
	if model == "" {
		model = "claude-3-5-haiku"
	}

	latencyMs, actualModel, err := s.puter.PingToken(r.Context(), token, model)
	if err != nil {
		if payload.AccountID != "" && strings.Contains(err.Error(), "402") {
			s.manager.MarkPuterAccountStatus(payload.AccountID, "cooldown", 24*time.Hour, err.Error())
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"status":     "error",
			"message":    err.Error(),
			"latency_ms": latencyMs,
			"model":      actualModel,
		})
		return
	}

	if payload.AccountID != "" {
		_ = s.manager.ResetPuterAccount(payload.AccountID)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"message":    "Token Puter valid & terhubung!",
		"latency_ms": latencyMs,
		"model":      actualModel,
	})
}

func (s *Server) handlePuterAccountsGet(w http.ResponseWriter, _ *http.Request) {
	accounts := s.manager.GetPuterAccounts()
	writeJSON(w, http.StatusOK, map[string]any{
		"accounts": accounts,
		"count":    len(accounts),
	})
}

func (s *Server) handlePuterAccountsPost(w http.ResponseWriter, r *http.Request) {
	var acc config.PuterAccount
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<18)).Decode(&acc); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}
	if err := s.manager.AddOrUpdatePuterAccount(acc); err != nil {
		writeError(w, http.StatusBadRequest, "account_save_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"message":  "Akun Puter berhasil disimpan ke pool",
		"accounts": s.manager.GetPuterAccounts(),
	})
}

func (s *Server) handlePuterAccountsDelete(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Parameter id diperlukan")
		return
	}
	if err := s.manager.DeletePuterAccount(id); err != nil {
		writeError(w, http.StatusInternalServerError, "account_delete_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"message":  "Akun berhasil dihapus dari pool",
		"accounts": s.manager.GetPuterAccounts(),
	})
}

func (s *Server) handlePuterAccountsReset(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing_id", "Parameter id diperlukan")
		return
	}
	if err := s.manager.ResetPuterAccount(id); err != nil {
		writeError(w, http.StatusNotFound, "account_not_found", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"message":  "Status akun berhasil di-reset menjadi Aktif",
		"accounts": s.manager.GetPuterAccounts(),
	})
}

func (s *Server) handlePuterAccountsPriority(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	priorityStr := r.URL.Query().Get("priority")
	if id == "" || priorityStr == "" {
		writeError(w, http.StatusBadRequest, "missing_params", "Parameter id dan priority diperlukan")
		return
	}
	var priority int
	if _, err := fmt.Sscanf(priorityStr, "%d", &priority); err != nil || priority < 1 {
		writeError(w, http.StatusBadRequest, "invalid_priority", "Priority harus berupa angka positif")
		return
	}
	if err := s.manager.SetPuterAccountPriority(id, priority); err != nil {
		writeError(w, http.StatusInternalServerError, "priority_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":   "ok",
		"message":  "Prioritas akun berhasil diperbarui",
		"accounts": s.manager.GetPuterAccounts(),
	})
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
