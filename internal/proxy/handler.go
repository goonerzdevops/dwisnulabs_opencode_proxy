package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"opencode-proxy/internal/config"
)

// maxBodyBytes caps client request bodies (large tool schemas + attachments).
const maxBodyBytes = 32 << 20

// fingerprintFor derives the upstream session identity. A caller-supplied
// session identifier is translated deterministically; otherwise the session is
// pinned to the caller identity so free-tier quota is not reset per request.
func (s *Server) fingerprintFor(r *http.Request, body map[string]any) Fingerprint {
	cfg := s.manager.Get()
	client := firstNonEmpty(
		r.Header.Get("x-opencode-client-name"),
		r.Header.Get("x-client-name"),
		r.Header.Get("x-app"),
		"generic",
	)

	for _, candidate := range []string{
		r.Header.Get("x-opencode-session-id"),
		r.Header.Get("x-opencode-session"),
		r.Header.Get("x-session-id"),
		r.Header.Get("x-session-affinity"),
		stringField(body, "session_id"),
		stringField(body, "conversation_id"),
	} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return Fingerprint{
				SessionID: TranslateSessionID(trimmed, client),
				Project:   s.projectFor(r, cfg),
			}
		}
	}

	identity := client + "|" + presentedKey(r) + "|" + clientHost(r)
	return Fingerprint{SessionID: s.sessions.Stable(identity), Project: s.projectFor(r, cfg)}
}

func (s *Server) projectFor(r *http.Request, cfg config.Config) string {
	if project := strings.TrimSpace(r.Header.Get("x-opencode-project")); project != "" {
		return project
	}
	_ = cfg
	return UpstreamProject
}

func clientHost(r *http.Request) string {
	raw := r.RemoteAddr
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		raw = strings.TrimSpace(strings.Split(forwarded, ",")[0])
	}
	if host, _, err := net.SplitHostPort(raw); err == nil {
		raw = host
	}
	if raw == "127.0.0.1" || raw == "::1" || raw == "localhost" {
		return "127.0.0.1"
	}
	return raw
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// handleChatCompletions is the single OpenAI-compatible entry point.
func (s *Server) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	cfg := s.manager.Get()

	body, err := decodeJSONBody(w, r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}

	prepared, err := Prepare(cfg, body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request_error", err.Error())
		return
	}
	s.requests.Add(1)

	fp := s.fingerprintFor(r, body)
	ctx, cancel := context.WithTimeout(r.Context(), time.Duration(cfg.UpstreamTimeoutSeconds)*time.Second)
	defer cancel()

	started := time.Now()
	resp, err := s.upstream.Send(ctx, cfg, prepared, fp)
	if err != nil {
		writeError(w, http.StatusBadGateway, "upstream_unreachable", err.Error())
		return
	}

	if cfg.LogRequests {
		s.logger.Printf("%s model=%s lane=%s stream=%t session=%s status=%d elapsed=%s",
			r.RemoteAddr, prepared.Model, prepared.Lane, prepared.ClientStreams, fp.SessionID,
			resp.StatusCode, time.Since(started).Round(time.Millisecond))
	}

	if resp.StatusCode != http.StatusOK {
		dump, _ := json.MarshalIndent(map[string]any{"raw": body, "prepared": prepared.Body}, "", "  ")
		_ = os.WriteFile("logs/last_failed_request.json", dump, 0644)
		s.forwardUpstreamError(w, resp)
		return
	}

	if prepared.Lane == LaneResponses {
		s.serveResponsesLane(w, prepared, resp, started)
		return
	}
	s.serveChatLane(w, prepared, resp, started)
}

// serveResponsesLane bridges the upstream Responses stream onto the client,
// either as SSE chunks or as one aggregated body.
func (s *Server) serveResponsesLane(w http.ResponseWriter, prepared *PreparedRequest, resp *http.Response, started time.Time) {
	bridge := NewResponsesBridge(prepared.Model, prepared.Renames, prepared.ClientStreams, w)
	scanErr := ScanSSE(resp.Body, func(event SSEEvent) (bool, error) {
		return false, bridge.HandleEvent(event.Event, event.Data)
	})
	resp.Body.Close()

	if failure := bridge.Failure(); failure != nil {
		if bridge.Written() {
			writeStreamError(w, failure)
			return
		}
		writeErrorPayload(w, http.StatusBadGateway, failure)
		return
	}
	if scanErr != nil {
		if bridge.Written() {
			writeStreamError(w, map[string]any{"message": scanErr.Error(), "type": "stream_error"})
			return
		}
		writeError(w, http.StatusBadGateway, "stream_error", scanErr.Error())
		return
	}
	bridge.Finish()

	prompt, cached, output := bridge.UsageTokens()
	s.telemetry.Record(RequestRecord{
		ID:             bridge.completionID(),
		Timestamp:      started,
		RequestedModel: prepared.Model,
		ActualModel:    bridge.ActualModel(),
		Lane:           string(prepared.Lane),
		Stream:         prepared.ClientStreams,
		PromptTokens:   prompt,
		CachedTokens:   cached,
		OutputTokens:   output,
		DurationMs:     time.Since(started).Milliseconds(),
		StatusCode:     http.StatusOK,
	})
}

// serveChatLane passes the upstream chat response through, aggregating the forced
// SSE stream when the client asked for a single JSON body.
func (s *Server) serveChatLane(w http.ResponseWriter, prepared *PreparedRequest, resp *http.Response, started time.Time) {
	if prepared.ClientStreams {
		defer resp.Body.Close()
		copyHeaders(w.Header(), resp.Header)
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.WriteHeader(http.StatusOK)

		actualModel, prompt, cached, output, _ := StreamChatSSEWithTelemetry(w, resp.Body, prepared.Renames)
		if actualModel == "" {
			actualModel = prepared.Model
		}
		s.telemetry.Record(RequestRecord{
			ID:             "req-" + randomBase62(16),
			Timestamp:      started,
			RequestedModel: prepared.Model,
			ActualModel:    actualModel,
			Lane:           string(prepared.Lane),
			Stream:         true,
			PromptTokens:   prompt,
			CachedTokens:   cached,
			OutputTokens:   output,
			DurationMs:     time.Since(started).Milliseconds(),
			StatusCode:     http.StatusOK,
		})
		return
	}

	aggregator := NewChatAggregator(prepared.Model, prepared.Renames)
	scanErr := ScanSSE(resp.Body, func(event SSEEvent) (bool, error) {
		return false, aggregator.HandleData(event.Data)
	})
	resp.Body.Close()

	if failure := aggregator.Failure(); failure != nil {
		writeErrorPayload(w, http.StatusBadGateway, failure)
		return
	}
	if scanErr != nil {
		writeError(w, http.StatusBadGateway, "stream_error", scanErr.Error())
		return
	}
	writeJSON(w, http.StatusOK, aggregator.Result())

	prompt, cached, output := aggregator.UsageTokens()
	s.telemetry.Record(RequestRecord{
		ID:             aggregator.id,
		Timestamp:      started,
		RequestedModel: prepared.Model,
		ActualModel:    aggregator.ActualModel(),
		Lane:           string(prepared.Lane),
		Stream:         false,
		PromptTokens:   prompt,
		CachedTokens:   cached,
		OutputTokens:   output,
		DurationMs:     time.Since(started).Milliseconds(),
		StatusCode:     http.StatusOK,
	})
}

// handleAnthropicMessages keeps the endpoint honest instead of silently
// returning an OpenAI-shaped body to an Anthropic client.
func (s *Server) handleAnthropicMessages(w http.ResponseWriter, _ *http.Request) {
	writeError(w, http.StatusNotImplemented, "not_implemented",
		"The Anthropic /v1/messages surface is not exposed by this proxy; use /v1/chat/completions with an OpenAI-compatible model id.")
}

// forwardUpstreamError relays a non-200 upstream body to the client, preserving
// the free-tier error type so clients can distinguish 403/429 causes.
func (s *Server) forwardUpstreamError(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()
	blob, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var envelope struct {
		Error map[string]any `json:"error"`
	}
	if err := json.Unmarshal(blob, &envelope); err == nil && len(envelope.Error) > 0 {
		s.logger.Printf("upstream error %d: %s", resp.StatusCode, truncate(string(blob), 400))
		writeErrorPayload(w, normalizeUpstreamStatus(resp.StatusCode), envelope.Error)
		return
	}
	message := strings.TrimSpace(string(blob))
	if message == "" {
		message = http.StatusText(resp.StatusCode)
	}
	s.logger.Printf("upstream error %d: %s", resp.StatusCode, truncate(message, 400))
	writeError(w, normalizeUpstreamStatus(resp.StatusCode), "upstream_error", message)
}

// normalizeUpstreamStatus keeps meaningful gate codes (403/429) and coerces
// anything nonsensical to 502.
func normalizeUpstreamStatus(status int) int {
	if status >= 400 && status < 600 {
		return status
	}
	return http.StatusBadGateway
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request) (map[string]any, error) {
	blob, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	blob = bytes.TrimPrefix(blob, []byte("\xef\xbb\xbf"))
	var body map[string]any
	if err := json.Unmarshal(blob, &body); err != nil {
		return nil, fmt.Errorf("invalid JSON body: %w", err)
	}
	if body == nil {
		return nil, fmt.Errorf("request body must be a JSON object")
	}
	return body, nil
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.Encode(payload)
}

// writeError emits the OpenAI error envelope clients expect.
func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]any{
			"message": message,
			"type":    code,
			"code":    code,
		},
	})
}

// writeErrorPayload relays an upstream error object, guaranteeing the type,
// code and message keys clients rely on.
func writeErrorPayload(w http.ResponseWriter, status int, payload map[string]any) {
	normalized := make(map[string]any, len(payload)+3)
	for key, value := range payload {
		normalized[key] = value
	}
	if stringField(normalized, "message") == "" {
		normalized["message"] = "upstream request failed"
	}
	if stringField(normalized, "type") == "" {
		normalized["type"] = "upstream_error"
	}
	if stringField(normalized, "code") == "" {
		if code := stringField(normalized, "type"); code != "" {
			normalized["code"] = code
		}
	}
	writeJSON(w, status, map[string]any{"error": normalized})
}

// writeStreamError terminates an already-open SSE response with an error frame.
func writeStreamError(w http.ResponseWriter, payload map[string]any) {
	normalized := make(map[string]any, len(payload)+1)
	for key, value := range payload {
		normalized[key] = value
	}
	if stringField(normalized, "type") == "" {
		normalized["type"] = "upstream_error"
	}
	blob, err := json.Marshal(map[string]any{"error": normalized})
	if err != nil {
		return
	}
	w.Write([]byte("data: "))
	w.Write(blob)
	w.Write([]byte("\n\n"))
	w.Write([]byte("data: [DONE]\n\n"))
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
}

