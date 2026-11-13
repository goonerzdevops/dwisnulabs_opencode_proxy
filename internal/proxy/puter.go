package proxy

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// KnownPuterModels lists verified working and curated latest flagship models provided via Puter AI.
var KnownPuterModels = []string{
	// Anthropic Claude (Opus 2 latest, Sonnet 2 latest, Haiku 2 latest)
	"claude-opus-5-5",
	"claude-opus-5",
	"claude-sonnet-5-5",
	"claude-sonnet-5",
	"claude-haiku-5-5",
	"claude-haiku-5.5",
	"claude-haiku-4.5",
	"claude-haiku-4-5-20251001",
	"claude-fable-5",

	// DeepSeek (deepseek-v4.1-flash-1017, deepseek-v4.1-flash, v4 pro, reasoning)
	"deepseek-v4.1-flash-1017",
	"infron:deepseek/deepseek-v4.1-flash-1017",
	"deepseek-v4.1-flash",
	"deepseek-v4-pro",
	"deepseek-v4-pro-0813",
	"deepseek-chat",
	"deepseek-reasoner",

	// OpenAI GPT (all latest generations: GPT-5.x, GPT-4o, and o-series)
	"gpt-5.4",
	"gpt-5.4-pro",
	"gpt-5.4-mini",
	"gpt-5.3-codex",
	"gpt-5.2",
	"gpt-5.1",
	"gpt-4o",
	"gpt-4o-mini",
	"o1",
	"o3-mini",

	// Google Gemini (latest versions)
	"gemini-3.8-flash",
	"gemini-3.1-pro-preview",
	"gemini-2.5-pro",
	"gemini-2.5-flash",

	// Alibaba Qwen (latest versions)
	"qwen3.5-flash",
	"qwen3.5-397b-a17b",
	"qwen3-coder-flash",

	// Zhipu GLM (latest versions)
	"glm-5.3-prime",
	"glm-5.2-fast-preview",
}

// IsPuterModel detects whether a model ID is intended for the Puter AI provider.
func IsPuterModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if strings.HasPrefix(m, "puter:") || strings.HasPrefix(m, "puter/") {
		return true
	}
	// OpenCode free-tier models (e.g. glm-5.3-flash-free, deepseek-v4.1-flash-free)
	// and native OpenCode models must never be hijacked by Puter prefix matching.
	if strings.HasSuffix(m, "-free") {
		return false
	}
	for _, om := range knownActiveModels {
		if strings.EqualFold(om, m) {
			return false
		}
	}
	if responsesModels[m] || messagesModels[m] {
		return false
	}
	if strings.HasPrefix(m, "claude-") {
		return true
	}
	if strings.HasPrefix(m, "infron:") || strings.HasPrefix(m, "infron/") {
		return true
	}
	if strings.HasPrefix(m, "gpt-") || strings.HasPrefix(m, "o1") || strings.HasPrefix(m, "o3") {
		return true
	}
	if strings.HasPrefix(m, "gemini-") {
		return true
	}
	if strings.HasPrefix(m, "qwen") {
		return true
	}
	if strings.HasPrefix(m, "glm-") {
		return true
	}
	if strings.Contains(m, "deepseek") {
		return true
	}
	for _, km := range KnownPuterModels {
		if strings.EqualFold(km, m) {
			return true
		}
	}
	return false
}

// CleanPuterModel strips any provider prefixes (e.g. "puter:claude-3-5-sonnet" -> "claude-3-5-sonnet").
func CleanPuterModel(model string) string {
	m := strings.TrimSpace(model)
	if strings.HasPrefix(m, "puter:") {
		m = strings.TrimPrefix(m, "puter:")
	} else if strings.HasPrefix(m, "puter/") {
		m = strings.TrimPrefix(m, "puter/")
	}
	// Map bare deepseek-v4.1-flash-1017 to upstream Puter catalog ID
	if m == "deepseek-v4.1-flash-1017" {
		return "infron:deepseek/deepseek-v4.1-flash-1017"
	}
	if m == "claude-haiku-5.5" || m == "haiku-5.5" || m == "haiku" {
		return "claude-haiku-5-5"
	}
	if m == "claude-haiku-4.5" || m == "haiku-4.5" {
		return "claude-haiku-4-5-20251001"
	}
	return m
}

// PuterService handles Puter catalog management.
type PuterService struct {
	logger *log.Logger
	mu     sync.Mutex
}

// NewPuterService creates a PuterService.
func NewPuterService(logger *log.Logger) *PuterService {
	if logger == nil {
		logger = log.Default()
	}
	return &PuterService{
		logger: logger,
	}
}

// FetchModels returns the curated Puter model entries.
func (p *PuterService) FetchModels(ctx context.Context, forceRefresh ...bool) []ModelEntry {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now().Unix()
	entries := make([]ModelEntry, 0, len(KnownPuterModels))
	for _, id := range KnownPuterModels {
		entries = append(entries, ModelEntry{
			ID:      id,
			Object:  "model",
			Created: now,
			OwnedBy: "puter",
		})
	}
	return entries
}

// forwardPuterChat forwards Puter chat completions via the Puter.js Browser Relay Bridge.
// Free Puter AI requires browser SDK execution (puter.ai.chat) via Browser Bridge
// because upstream REST api.puter.com requires a paid subscription.
func (s *Server) forwardPuterChat(w http.ResponseWriter, r *http.Request, body map[string]any) {
	cfg := s.manager.Get()
	started := time.Now()
	s.requests.Add(1)

	// Determine model
	reqModel := stringField(body, "model")
	if reqModel == "" {
		reqModel = cfg.PuterDefaultModel
	}
	if reqModel == "" {
		reqModel = "claude-opus-5-5"
	}
	actualModel := CleanPuterModel(reqModel)
	body["model"] = actualModel

	isStream, _ := body["stream"].(bool)

	if s.bridge == nil {
		s.logger.Printf("[PUTER RELAY] Browser Bridge instance is nil")
		writeError(w, http.StatusServiceUnavailable, "browser_bridge_unavailable",
			"Browser Relay Puter belum aktif. Buka Web UI proxy di http://127.0.0.1:20144 pada tab Puter AI Hub untuk menghubungkan browser relay.")
		return
	}

	ready, username := s.bridge.IsReady()
	// If tab is connected but ready handshake is in flight, wait up to 3 seconds
	if !ready && s.bridge.IsConnected() {
		for i := 0; i < 15; i++ {
			time.Sleep(200 * time.Millisecond)
			if rdy, u := s.bridge.IsReady(); rdy {
				ready = true
				username = u
				break
			}
		}
	}

	if !ready {
		if !s.bridge.IsConnected() {
			s.logger.Printf("[PUTER RELAY] Browser Bridge not connected for model=%s", actualModel)
			writeError(w, http.StatusServiceUnavailable, "browser_bridge_not_connected",
				"Browser Relay Puter belum terhubung. Tab Web UI proxy belum dibuka atau browser tab terputus. Buka Web UI di http://127.0.0.1:20144 pada tab Puter AI Hub dan pastikan browser terhubung & login ke Puter.")
			return
		}
		s.logger.Printf("[PUTER RELAY] Browser Bridge connected but not signed in (model=%s)", actualModel)
		writeError(w, http.StatusServiceUnavailable, "browser_bridge_not_signed_in",
			"Browser Relay Puter terhubung tetapi belum login ke Puter. Buka Web UI di http://127.0.0.1:20144 pada tab Puter AI Hub dan klik tombol 'Login ke Puter' agar request dapat dieksekusi via 1.000 kredit gratis browser SDK.")
		return
	}

	s.logger.Printf("[PUTER RELAY] Delegating model=%s stream=%t to Browser Bridge (user: %s)", actualModel, isStream, username)
	if err := s.bridge.Execute(r.Context(), actualModel, body, w); err != nil {
		s.logger.Printf("[PUTER RELAY] Browser Bridge execution failed: %v", err)
		writeError(w, http.StatusBadGateway, "browser_bridge_error", fmt.Sprintf("Eksekusi via Browser Relay Puter gagal: %v", err))
		return
	}

	s.telemetry.Record(RequestRecord{
		ID:             "req-ptr-relay-" + randomBase62(12),
		Timestamp:      started,
		RequestedModel: reqModel,
		ActualModel:    actualModel,
		Lane:           "puter-browser-relay",
		Stream:         isStream,
		DurationMs:     time.Since(started).Milliseconds(),
		StatusCode:     http.StatusOK,
	})
}
