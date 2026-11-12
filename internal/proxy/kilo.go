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
)

const (
	KiloBaseURL   = "https://api.kilo.ai"
	KiloChatURL   = "https://api.kilo.ai/api/gateway/chat/completions"
	KiloModelsURL = "https://api.kilo.ai/api/gateway/models"
)

// KnownKiloModels lists verified free models provided via Kilo Gateway.
var KnownKiloModels = []string{
	"openrouter/free",
	"kilo-auto/free",
	"nvidia/nemotron-3-ultra-550b-a55b:free",
	"nvidia/nemotron-3.5-super-vl:free",
	"stepfun/step-3.7-flash:free",
	"poolside/laguna-xs-2.1:free",
}

// IsKiloModel detects whether a model ID is intended for the Kilo Gateway provider.
func IsKiloModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	if strings.HasPrefix(m, "kilo:") || strings.HasPrefix(m, "kilo/") {
		return true
	}
	if strings.HasSuffix(m, ":free") || strings.HasSuffix(m, "/free") {
		return true
	}
	for _, km := range KnownKiloModels {
		if strings.EqualFold(km, m) {
			return true
		}
	}
	return false
}

// CleanKiloModel strips any provider prefixes (e.g. "kilo:openrouter/free" -> "openrouter/free").
func CleanKiloModel(model string) string {
	m := strings.TrimSpace(model)
	if strings.HasPrefix(m, "kilo:") {
		return strings.TrimPrefix(m, "kilo:")
	}
	if strings.HasPrefix(m, "kilo/") {
		return strings.TrimPrefix(m, "kilo/")
	}
	return m
}

// KiloService handles interaction with api.kilo.ai.
type KiloService struct {
	client    *http.Client
	logger    *log.Logger
	mu        sync.Mutex
	cached    []ModelEntry
	fetchedAt time.Time
}

// NewKiloService creates a KiloService with default HTTP client.
func NewKiloService(logger *log.Logger) *KiloService {
	if logger == nil {
		logger = log.Default()
	}
	return &KiloService{
		client: &http.Client{
			Timeout: 180 * time.Second,
		},
		logger: logger,
	}
}

// SetHTTPClient sets a custom HTTP client, primarily for unit testing.
func (k *KiloService) SetHTTPClient(client *http.Client) {
	if client != nil {
		k.client = client
	}
}

// FetchModels returns Kilo model entries, fetching from live Kilo catalog if available.
func (k *KiloService) FetchModels(ctx context.Context, forceRefresh ...bool) []ModelEntry {
	force := len(forceRefresh) > 0 && forceRefresh[0]
	k.mu.Lock()
	stale := force || time.Since(k.fetchedAt) > 10*time.Minute || len(k.cached) == 0
	cached := k.cached
	k.mu.Unlock()

	if stale {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, KiloModelsURL, nil)
		if err == nil {
			req.Header.Set("Accept", "application/json")
			req.Header.Set("User-Agent", "opencode-proxy/1.0")
			if resp, err := k.client.Do(req); err == nil {
				if resp.StatusCode == http.StatusOK {
					var catalog struct {
						Data []struct {
							ID      string `json:"id"`
							Name    string `json:"name"`
							IsFree  bool   `json:"isFree"`
							Created int64  `json:"created"`
						} `json:"data"`
					}
					if json.NewDecoder(resp.Body).Decode(&catalog) == nil && len(catalog.Data) > 0 {
						now := time.Now().Unix()
						seen := make(map[string]bool)
						var entries []ModelEntry

						for _, km := range KnownKiloModels {
							seen[km] = true
							entries = append(entries, ModelEntry{
								ID:      km,
								Object:  "model",
								Created: now,
								OwnedBy: "kilo",
							})
						}

						for _, m := range catalog.Data {
							id := strings.TrimSpace(m.ID)
							if id == "" {
								continue
							}
							isFree := m.IsFree || strings.HasSuffix(id, ":free") || strings.HasSuffix(id, "/free")
							if isFree && !seen[id] {
								seen[id] = true
								created := m.Created
								if created <= 0 {
									created = now
								}
								entries = append(entries, ModelEntry{
									ID:      id,
									Object:  "model",
									Created: created,
									OwnedBy: "kilo",
								})
							}
						}
						resp.Body.Close()

						k.mu.Lock()
						k.cached = entries
						k.fetchedAt = time.Now()
						k.mu.Unlock()
						return entries
					}
				}
				resp.Body.Close()
			}
		}
	}

	if len(cached) > 0 {
		return cached
	}

	now := time.Now().Unix()
	entries := make([]ModelEntry, 0, len(KnownKiloModels))
	for _, id := range KnownKiloModels {
		entries = append(entries, ModelEntry{
			ID:      id,
			Object:  "model",
			Created: now,
			OwnedBy: "kilo",
		})
	}
	return entries
}

// forwardKiloChat forwards an OpenAI-compatible request directly to Kilo Gateway upstream.
func (s *Server) forwardKiloChat(w http.ResponseWriter, r *http.Request, body map[string]any) {
	cfg := s.manager.Get()
	started := time.Now()
	s.requests.Add(1)

	// Determine model
	reqModel := stringField(body, "model")
	if reqModel == "" {
		reqModel = cfg.KiloDefaultModel
	}
	if reqModel == "" {
		reqModel = "openrouter/free"
	}
	actualModel := CleanKiloModel(reqModel)
	body["model"] = actualModel

	// Prepare clean JSON payload
	rawPayload, err := json.Marshal(body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_payload", err.Error())
		return
	}

	isStream, _ := body["stream"].(bool)

	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.UpstreamTimeoutSeconds)*time.Second)
	defer cancel()

	upReq, err := http.NewRequestWithContext(ctx, http.MethodPost, KiloChatURL, bytes.NewReader(rawPayload))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "request_create_failed", err.Error())
		return
	}

	upReq.Header.Set("Content-Type", "application/json")
	if isStream {
		upReq.Header.Set("Accept", "text/event-stream")
	} else {
		upReq.Header.Set("Accept", "application/json")
	}
	upReq.Header.Set("User-Agent", "opencode-proxy/1.0")

	// Keyless: do not set Authorization header.

	resp, err := s.kilo.client.Do(upReq)
	if err != nil {
		s.logger.Printf("[KILO] Upstream unreachable: %v", err)
		writeError(w, http.StatusBadGateway, "kilo_upstream_unreachable",
			fmt.Sprintf("Kilo Gateway upstream is unreachable: %v", err))
		return
	}

	// Non-200 response (e.g. 429 rate limit or 400 bad request)
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		blob, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(resp.StatusCode)
		w.Write(blob)

		s.telemetry.Record(RequestRecord{
			ID:             "req-kilo-" + randomBase62(16),
			Timestamp:      started,
			RequestedModel: reqModel,
			ActualModel:    actualModel,
			Lane:           "kilo",
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

	// 200 OK - Streaming
	if isStream {
		defer resp.Body.Close()
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
			ID:             "req-kilo-" + randomBase62(16),
			Timestamp:      started,
			RequestedModel: reqModel,
			ActualModel:    actualModel,
			Lane:           "kilo",
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

	// 200 OK - Non-streaming
	defer resp.Body.Close()
	blob, _ := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	w.Write(blob)

	var respData struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(blob, &respData)

	s.telemetry.Record(RequestRecord{
		ID:             "req-kilo-" + randomBase62(16),
		Timestamp:      started,
		RequestedModel: reqModel,
		ActualModel:    actualModel,
		Lane:           "kilo",
		Stream:         false,
		PromptTokens:   respData.Usage.PromptTokens,
		CachedTokens:   0,
		OutputTokens:   respData.Usage.CompletionTokens,
		TotalTokens:    respData.Usage.TotalTokens,
		DurationMs:     time.Since(started).Milliseconds(),
		StatusCode:     http.StatusOK,
	})
}

