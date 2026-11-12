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
	"sync"
	"time"

	"opencode-proxy/internal/config"
)

const (
	PuterChatURL   = "https://api.puter.com/puterai/openai/v1/chat/completions"
	PuterModelsURL = "https://api.puter.com/puterai/chat/models/details"
)

// KnownPuterModels lists verified working models provided via Puter AI.
var KnownPuterModels = []string{
	"claude-opus-5-5",
	"claude-sonnet-5-5",
	"claude-opus-5",
	"claude-haiku-5-5",
	"claude-fable-5",
	"gpt-4o",
	"gpt-4o-mini",
	"deepseek-v4.1-flash",
	"deepseek-chat",
	"deepseek-reasoner",
	"gemini-2.0-flash",
}

// IsPuterModel detects whether a model ID is intended for the Puter AI provider.
func IsPuterModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if strings.HasPrefix(m, "puter:") || strings.HasPrefix(m, "puter/") {
		return true
	}
	if strings.HasPrefix(m, "claude-") {
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
		return strings.TrimPrefix(m, "puter:")
	}
	if strings.HasPrefix(m, "puter/") {
		return strings.TrimPrefix(m, "puter/")
	}
	return m
}

// PuterService handles interaction with api.puter.com.
type PuterService struct {
	client    *http.Client
	logger    *log.Logger
	mu        sync.Mutex
	cached    []ModelEntry
	fetchedAt time.Time
}

// NewPuterService creates a PuterService with a reasonable transport timeout.
func NewPuterService(logger *log.Logger) *PuterService {
	if logger == nil {
		logger = log.Default()
	}
	return &PuterService{
		client: &http.Client{
			Timeout: 180 * time.Second,
		},
		logger: logger,
	}
}

// PingToken sends a tiny 5-token ping request to verify the Puter token validity and measure latency.
func (p *PuterService) PingToken(ctx context.Context, token string, testModel string) (int64, string, error) {
	cleanToken := strings.TrimSpace(token)
	if cleanToken == "" {
		return 0, "", fmt.Errorf("token cannot be empty")
	}

	model := strings.TrimSpace(testModel)
	if model == "" {
		model = "claude-3-5-haiku"
	}
	model = CleanPuterModel(model)

	payload := map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "ping"},
		},
		"max_tokens": 5,
		"stream":     false,
	}

	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, "", fmt.Errorf("marshal ping payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, PuterChatURL, bytes.NewReader(raw))
	if err != nil {
		return 0, "", fmt.Errorf("create ping request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+cleanToken)

	start := time.Now()
	resp, err := p.client.Do(req)
	latencyMs := time.Since(start).Milliseconds()
	if err != nil {
		return latencyMs, model, fmt.Errorf("ping connection error: %w", err)
	}
	defer resp.Body.Close()

	respBlob, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<18))

	if resp.StatusCode != http.StatusOK {
		var errData struct {
			Error struct {
				Message string `json:"message"`
				Type    string `json:"type"`
			} `json:"error"`
			Message string `json:"message"`
		}
		errMsg := strings.TrimSpace(string(respBlob))
		if json.Unmarshal(respBlob, &errData) == nil {
			if errData.Error.Message != "" {
				errMsg = errData.Error.Message
			} else if errData.Message != "" {
				errMsg = errData.Message
			}
		}
		return latencyMs, model, fmt.Errorf("puter upstream error (%d): %s", resp.StatusCode, errMsg)
	}

	return latencyMs, model, nil
}

// FetchModels returns Puter model entries, fetching from live Puter catalog if available.
func (p *PuterService) FetchModels(ctx context.Context, token string, forceRefresh ...bool) []ModelEntry {
	force := len(forceRefresh) > 0 && forceRefresh[0]
	p.mu.Lock()
	stale := force || time.Since(p.fetchedAt) > 10*time.Minute || len(p.cached) == 0
	cached := p.cached
	p.mu.Unlock()

	if stale {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, PuterModelsURL, nil)
		if err == nil {
			if resp, err := p.client.Do(req); err == nil && resp.StatusCode == http.StatusOK {
				var catalog struct {
					Models []struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"models"`
				}
				if json.NewDecoder(resp.Body).Decode(&catalog) == nil && len(catalog.Models) > 0 {
					now := time.Now().Unix()
					seen := make(map[string]bool)
					var entries []ModelEntry

					for _, km := range KnownPuterModels {
						seen[km] = true
						entries = append(entries, ModelEntry{
							ID:      km,
							Object:  "model",
							Created: now,
							OwnedBy: "puter",
						})
					}

					for _, m := range catalog.Models {
						id := strings.TrimSpace(m.ID)
						if id == "" {
							id = strings.TrimSpace(m.Name)
						}
						if id != "" && !seen[id] {
							seen[id] = true
							entries = append(entries, ModelEntry{
								ID:      id,
								Object:  "model",
								Created: now,
								OwnedBy: "puter",
							})
						}
					}
					resp.Body.Close()

					p.mu.Lock()
					p.cached = entries
					p.fetchedAt = time.Now()
					p.mu.Unlock()
					return entries
				}
				resp.Body.Close()
			}
		}
	}

	if len(cached) > 0 {
		return cached
	}

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

// ForwardChat forwards an OpenAI-compatible request directly to Puter upstream with auto-failover across accounts.
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
		reqModel = "claude-3-5-sonnet"
	}
	actualModel := CleanPuterModel(reqModel)
	body["model"] = actualModel

	// Build candidate accounts pool
	var candidateTokens []struct {
		AccountID string
		Username  string
		Token     string
	}

	authKey := presentedKey(r)
	if authKey != "" && authKey != "sk-opencode-free" && !isKnownLocalKey(authKey, cfg) {
		candidateTokens = append(candidateTokens, struct {
			AccountID string
			Username  string
			Token     string
		}{
			AccountID: "client-bearer",
			Username:  "client_bearer",
			Token:     authKey,
		})
	} else {
		candidates := s.manager.GetActivePuterAccountCandidates()
		for _, c := range candidates {
			candidateTokens = append(candidateTokens, struct {
				AccountID string
				Username  string
				Token     string
			}{
				AccountID: c.ID,
				Username:  c.Username,
				Token:     c.Token,
			})
		}
		if len(candidateTokens) == 0 && cfg.PuterAPIKey != "" {
			candidateTokens = append(candidateTokens, struct {
				AccountID string
				Username  string
				Token     string
			}{
				AccountID: "acc-default",
				Username:  "default",
				Token:     cfg.PuterAPIKey,
			})
		}
	}

	if len(candidateTokens) == 0 {
		allAccs := s.manager.GetPuterAccounts()
		if len(allAccs) > 0 {
			writeError(w, http.StatusPaymentRequired, "all_puter_accounts_exhausted",
				"Semua akun Puter di pool telah kehabisan kredit (402) atau terkena rate limit (429). Silakan tambah akun baru atau reset status di tab Puter AI Hub.")
			return
		}
		writeError(w, http.StatusUnauthorized, "missing_puter_token",
			"Puter API token is not configured. Please enter your Puter token or add accounts in the Puter AI Hub tab.")
		return
	}

	// Prepare clean JSON payload
	rawPayload, err := json.Marshal(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_payload", err.Error())
		return
	}

	isStream, _ := body["stream"].(bool)

	var lastStatusCode int
	var lastErrorBlob []byte

	for idx, cand := range candidateTokens {
		ctx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.UpstreamTimeoutSeconds)*time.Second)

		upReq, err := http.NewRequestWithContext(ctx, http.MethodPost, PuterChatURL, bytes.NewReader(rawPayload))
		if err != nil {
			cancel()
			writeError(w, http.StatusInternalServerError, "request_create_failed", err.Error())
			return
		}

		upReq.Header.Set("Content-Type", "application/json")
		upReq.Header.Set("Authorization", "Bearer "+cand.Token)

		resp, err := http.DefaultClient.Do(upReq)
		if err != nil {
			cancel()
			s.logger.Printf("[PUTER] Account %s (%s) unreachable: %v", cand.Username, cand.AccountID, err)
			continue
		}

		// 402: Credits Exhausted -> Cooldown & Failover to next
		if resp.StatusCode == http.StatusPaymentRequired {
			lastStatusCode = resp.StatusCode
			lastErrorBlob, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<18))
			resp.Body.Close()
			cancel()

			s.manager.MarkPuterAccountStatus(cand.AccountID, "cooldown", 24*time.Hour, "HTTP 402: Insufficient credits")
			s.logger.Printf("[PUTER FAILOVER] Account %s (%s) exhausted credits (HTTP 402). Failing over to next account (%d/%d)...",
				cand.Username, cand.AccountID, idx+1, len(candidateTokens))
			continue
		}

		// 429: Rate Limited -> 60s Cooldown & Failover to next
		if resp.StatusCode == http.StatusTooManyRequests {
			lastStatusCode = resp.StatusCode
			lastErrorBlob, _ = io.ReadAll(io.LimitReader(resp.Body, 1<<18))
			resp.Body.Close()
			cancel()

			s.manager.MarkPuterAccountStatus(cand.AccountID, "rate_limited", 60*time.Second, "HTTP 429: Rate limited")
			s.logger.Printf("[PUTER FAILOVER] Account %s (%s) rate limited (HTTP 429). Failing over to next account (%d/%d)...",
				cand.Username, cand.AccountID, idx+1, len(candidateTokens))
			continue
		}

		// Non-200 non-failover error (e.g. 400 Bad Request)
		if resp.StatusCode != http.StatusOK {
			defer resp.Body.Close()
			defer cancel()
			blob, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(resp.StatusCode)
			w.Write(blob)

			s.telemetry.Record(RequestRecord{
				ID:             "req-ptr-" + randomBase62(16),
				Timestamp:      started,
				RequestedModel: reqModel,
				ActualModel:    actualModel,
				Lane:           "puter",
				Stream:         isStream,
				PromptTokens:   0,
				CachedTokens:   0,
				OutputTokens:   0,
				TotalTokens:    0,
				DurationMs:     time.Since(started).Milliseconds(),
				StatusCode:     resp.StatusCode,
			})
			return
		}

		// Success (200 OK)!
		s.manager.RecordPuterAccountUsage(cand.AccountID)
		elapsed := time.Since(started).Milliseconds()
		if cfg.LogRequests {
			s.logger.Printf("%s [PUTER] account=%s model=%s stream=%t status=200 elapsed=%dms",
				r.RemoteAddr, cand.Username, actualModel, isStream, elapsed)
		}

		if isStream {
			defer resp.Body.Close()
			defer cancel()
			copyHeaders(w.Header(), resp.Header)
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
			w.Header().Set("Cache-Control", "no-cache, no-transform")
			w.WriteHeader(http.StatusOK)

			flusher, isFlusher := w.(http.Flusher)
			buf := make([]byte, 4096)
			for {
				n, readErr := resp.Body.Read(buf)
				if n > 0 {
					w.Write(buf[:n])
					if isFlusher {
						flusher.Flush()
					}
				}
				if readErr != nil {
					break
				}
			}

			s.telemetry.Record(RequestRecord{
				ID:             "req-ptr-" + randomBase62(16),
				Timestamp:      started,
				RequestedModel: reqModel,
				ActualModel:    actualModel,
				Lane:           "puter",
				Stream:         true,
				PromptTokens:   0,
				CachedTokens:   0,
				OutputTokens:   0,
				TotalTokens:    0,
				DurationMs:     time.Since(started).Milliseconds(),
				StatusCode:     http.StatusOK,
			})
			return
		}

		defer resp.Body.Close()
		defer cancel()
		blob, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(blob)

		s.telemetry.Record(RequestRecord{
			ID:             "req-ptr-" + randomBase62(16),
			Timestamp:      started,
			RequestedModel: reqModel,
			ActualModel:    actualModel,
			Lane:           "puter",
			Stream:         false,
			PromptTokens:   0,
			CachedTokens:   0,
			OutputTokens:   0,
			TotalTokens:    0,
			DurationMs:     time.Since(started).Milliseconds(),
			StatusCode:     http.StatusOK,
		})
		return
	}

	// All candidate accounts failed
	if lastStatusCode != 0 && len(lastErrorBlob) > 0 {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(lastStatusCode)
		w.Write(lastErrorBlob)
	} else {
		writeError(w, http.StatusBadGateway, "all_puter_accounts_failed", "All candidate Puter accounts failed or returned errors.")
	}
}

func isKnownLocalKey(key string, cfg config.Config) bool {
	for _, k := range cfg.APIKeys {
		if k == key {
			return true
		}
	}
	return false
}

