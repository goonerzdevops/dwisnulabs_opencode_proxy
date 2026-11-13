package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// BridgeMessage represents an IPC packet between Go Proxy and the Browser Web UI worker.
type BridgeMessage struct {
	ID       string `json:"id,omitempty"`
	Action   string `json:"action,omitempty"` // "chat", "ping", "ready"
	Type     string `json:"type,omitempty"`   // "chunk", "done", "error"
	Model    string `json:"model,omitempty"`
	Messages any    `json:"messages,omitempty"`
	Stream   bool   `json:"stream,omitempty"`
	Text     string `json:"text,omitempty"`
	Error    string `json:"error,omitempty"`
	SignedIn bool   `json:"signed_in,omitempty"`
	Username string `json:"username,omitempty"`
}

// BrowserBridge relays client requests to the user's browser tab running puter.js via SSE + HTTP.
type BrowserBridge struct {
	logger       *log.Logger
	mu           sync.RWMutex
	eventCh      chan []byte
	signedIn     bool
	username     string
	connectedAt  time.Time
	jobsMu       sync.Mutex
	jobs         map[string]chan BridgeMessage
	relayedReqs  atomic.Int64
	tokensStream atomic.Int64
}

// NewBrowserBridge initializes the BrowserBridge.
func NewBrowserBridge(logger *log.Logger) *BrowserBridge {
	if logger == nil {
		logger = log.Default()
	}
	return &BrowserBridge{
		logger: logger,
		jobs:   make(map[string]chan BridgeMessage),
	}
}

// HandleEvents handles the persistent SSE stream to the browser tab.
func (b *BrowserBridge) HandleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "sse_not_supported", "Streaming not supported by server")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	ch := make(chan []byte, 100)

	b.mu.Lock()
	if b.eventCh != nil {
		close(b.eventCh)
	}
	b.eventCh = ch
	b.connectedAt = time.Now()
	b.mu.Unlock()

	b.logger.Printf("[BROWSER BRIDGE] Browser tab connected via SSE from %s", r.RemoteAddr)

	defer func() {
		b.mu.Lock()
		if b.eventCh == ch {
			b.eventCh = nil
			b.signedIn = false
			b.username = ""
		}
		b.mu.Unlock()
		b.logger.Printf("[BROWSER BRIDGE] Browser tab disconnected")
	}()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	// Initial connected ping
	fmt.Fprintf(w, ": connected\n\n")
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		case data, ok := <-ch:
			if !ok {
				return
			}
			w.Write(data)
			flusher.Flush()
		}
	}
}

// HandleReady receives auth status reports from the browser.
func (b *BrowserBridge) HandleReady(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		SignedIn bool   `json:"signed_in"`
		Username string `json:"username"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}

	b.mu.Lock()
	b.signedIn = payload.SignedIn
	b.username = payload.Username
	b.mu.Unlock()

	b.logger.Printf("[BROWSER BRIDGE] Worker reported ready (signed_in=%t, user=%s)", payload.SignedIn, payload.Username)
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// HandleChunk receives streaming delta chunks from the browser worker.
func (b *BrowserBridge) HandleChunk(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID   string `json:"id"`
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}

	if payload.ID != "" {
		b.jobsMu.Lock()
		ch, ok := b.jobs[payload.ID]
		b.jobsMu.Unlock()
		if ok && ch != nil {
			select {
			case ch <- BridgeMessage{ID: payload.ID, Type: "chunk", Text: payload.Text}:
			case <-time.After(3 * time.Second):
				b.logger.Printf("[BROWSER BRIDGE] WARN: job %s chunk buffer saturated, timed out pushing chunk", payload.ID)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// HandleDone receives completion signals from the browser worker.
func (b *BrowserBridge) HandleDone(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}

	if payload.ID != "" {
		b.jobsMu.Lock()
		ch, ok := b.jobs[payload.ID]
		b.jobsMu.Unlock()
		if ok && ch != nil {
			b.logger.Printf("[BROWSER BRIDGE] Job %s received DONE signal from browser worker", payload.ID)
			select {
			case ch <- BridgeMessage{ID: payload.ID, Type: "done"}:
			case <-time.After(5 * time.Second):
				b.logger.Printf("[BROWSER BRIDGE] WARN: job %s timed out pushing DONE to channel", payload.ID)
			}
		} else {
			b.logger.Printf("[BROWSER BRIDGE] WARN: job %s received DONE but job was not found in registry", payload.ID)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// HandleError receives execution error signals from the browser worker.
func (b *BrowserBridge) HandleError(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		ID    string `json:"id"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_payload", err.Error())
		return
	}

	if payload.ID != "" {
		b.jobsMu.Lock()
		ch, ok := b.jobs[payload.ID]
		b.jobsMu.Unlock()
		if ok && ch != nil {
			b.logger.Printf("[BROWSER BRIDGE] Job %s received ERROR from browser worker: %s", payload.ID, payload.Error)
			select {
			case ch <- BridgeMessage{ID: payload.ID, Type: "error", Error: payload.Error}:
			case <-time.After(5 * time.Second):
				b.logger.Printf("[BROWSER BRIDGE] WARN: job %s timed out pushing ERROR to channel", payload.ID)
			}
		} else {
			b.logger.Printf("[BROWSER BRIDGE] WARN: job %s received ERROR but job was not found: %s", payload.ID, payload.Error)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// HandleStatus returns the current live bridge status as JSON.
func (b *BrowserBridge) HandleStatus(w http.ResponseWriter, _ *http.Request) {
	b.mu.RLock()
	connected := b.eventCh != nil
	signedIn := b.signedIn
	username := b.username
	uptime := int(time.Since(b.connectedAt).Seconds())
	b.mu.RUnlock()

	if !connected {
		uptime = 0
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"connected":      connected,
		"signed_in":      signedIn,
		"username":       username,
		"uptime_seconds": uptime,
		"relayed_reqs":   b.relayedReqs.Load(),
	})
}

// IsReady checks if a browser worker is connected and logged in.
func (b *BrowserBridge) IsReady() (bool, string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.eventCh != nil && b.signedIn, b.username
}

// IsConnected checks if any browser tab is connected.
func (b *BrowserBridge) IsConnected() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.eventCh != nil
}

// Execute relays a chat completion request to the browser worker and streams/buffers the response.
func (b *BrowserBridge) Execute(ctx context.Context, model string, body map[string]any, w http.ResponseWriter) error {
	b.mu.RLock()
	eventCh := b.eventCh
	signedIn := b.signedIn
	username := b.username
	b.mu.RUnlock()

	if eventCh == nil {
		return errors.New("browser_bridge_not_connected")
	}
	if !signedIn {
		return errors.New("browser_bridge_not_signed_in")
	}

	jobID := "bridge-" + randomBase62(16)
	ch := make(chan BridgeMessage, 1000)

	b.jobsMu.Lock()
	b.jobs[jobID] = ch
	b.jobsMu.Unlock()

	defer func() {
		b.jobsMu.Lock()
		delete(b.jobs, jobID)
		b.jobsMu.Unlock()
	}()

	isStream, _ := body["stream"].(bool)
	messages := body["messages"]

	// Send execution command to browser worker via SSE event
	taskObj := BridgeMessage{
		ID:       jobID,
		Action:   "chat",
		Model:    model,
		Messages: messages,
		Stream:   isStream,
	}

	rawJSON, err := json.Marshal(taskObj)
	if err != nil {
		return fmt.Errorf("marshal bridge task failed: %w", err)
	}

	ssePacket := []byte(fmt.Sprintf("event: chat\ndata: %s\n\n", string(rawJSON)))
	select {
	case eventCh <- ssePacket:
		b.logger.Printf("[BROWSER BRIDGE] Dispatched job %s to browser worker (model=%s, stream=%t)", jobID, model, isStream)
	case <-time.After(5 * time.Second):
		b.logger.Printf("[BROWSER BRIDGE] ERROR: Job %s timed out dispatching event to browser SSE channel", jobID)
		return errors.New("browser_bridge_write_timeout")
	}

	b.relayedReqs.Add(1)
	createdNow := time.Now().Unix()

	if isStream {
		w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, no-transform")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)

		flusher, isFlusher := w.(http.Flusher)
		if isFlusher {
			flusher.Flush()
		}

		firstChunk := true
		filter := newToolMarkupFilter(extractToolSchemas(body))

		// emit writes one chat.completion.chunk to the client.
		emit := func(delta map[string]any, finishReason any) {
			if firstChunk {
				delta["role"] = "assistant"
				firstChunk = false
			}
			chunkObj := map[string]any{
				"id":      "chatcmpl-" + jobID,
				"object":  "chat.completion.chunk",
				"created": createdNow,
				"model":   model,
				"choices": []map[string]any{
					{
						"index":         0,
						"delta":         delta,
						"finish_reason": finishReason,
					},
				},
			}
			rawChunk, _ := json.Marshal(chunkObj)
			fmt.Fprintf(w, "data: %s\n\n", string(rawChunk))
			if isFlusher {
				flusher.Flush()
			}
		}

		pingTicker := time.NewTicker(15 * time.Second)
		defer pingTicker.Stop()

		idleTimer := time.NewTimer(120 * time.Second)
		defer idleTimer.Stop()

		chunkCount := 0
		for {
			select {
			case <-ctx.Done():
				b.logger.Printf("[BROWSER BRIDGE] Job %s client context cancelled: %v", jobID, ctx.Err())
				return ctx.Err()
			case <-pingTicker.C:
				// SSE comment keep-alive to prevent intermediate connection drops
				fmt.Fprint(w, ": keep-alive\n\n")
				if isFlusher {
					flusher.Flush()
				}
			case <-idleTimer.C:
				b.logger.Printf("[BROWSER BRIDGE] Job %s stream idle timeout: no data from browser worker for 120s", jobID)
				return fmt.Errorf("browser_bridge_timeout: browser worker did not respond within 120s for job %s", jobID)
			case msg, ok := <-ch:
				if !idleTimer.Stop() {
					select {
					case <-idleTimer.C:
					default:
					}
				}
				idleTimer.Reset(120 * time.Second)

				if !ok {
					return nil
				}
				if msg.Type == "error" {
					b.logger.Printf("[BROWSER BRIDGE] Job %s failed with error: %s", jobID, msg.Error)
					return errors.New(msg.Error)
				}
				if msg.Type == "done" {
					leftover, toolCalls := filter.Finish()
					if leftover != "" {
						emit(map[string]any{"content": leftover}, nil)
					}
					finishReason := "stop"
					if len(toolCalls) > 0 {
						b.logger.Printf("[BROWSER BRIDGE] Job %s extracted %d tool call(s) (first: %s), emitting finish_reason=tool_calls", jobID, len(toolCalls), toolCalls[0].Function.Name)
						emit(map[string]any{"tool_calls": toolCalls}, nil)
						finishReason = "tool_calls"
					}
					emit(map[string]any{}, finishReason)
					fmt.Fprint(w, "data: [DONE]\n\n")
					if isFlusher {
						flusher.Flush()
					}
					b.logger.Printf("[BROWSER BRIDGE] Job %s finished stream successfully (chunks: %d, tool_calls: %d)", jobID, chunkCount, len(toolCalls))
					return nil
				}
				if msg.Type == "chunk" && msg.Text != "" {
					chunkCount++
					if out := filter.Push(msg.Text); out != "" {
						emit(map[string]any{"content": out}, nil)
					}
				}
			}
		}
	}

	// Non-streaming response: accumulate chunks until done
	idleTimer := time.NewTimer(120 * time.Second)
	defer idleTimer.Stop()

	var fullText string
	for {
		select {
		case <-ctx.Done():
			b.logger.Printf("[BROWSER BRIDGE] Job %s client context cancelled: %v", jobID, ctx.Err())
			return ctx.Err()
		case <-idleTimer.C:
			b.logger.Printf("[BROWSER BRIDGE] Job %s idle timeout (non-stream): no response from browser worker for 120s", jobID)
			return fmt.Errorf("browser_bridge_timeout: browser worker did not respond within 120s for job %s", jobID)
		case msg, ok := <-ch:
			if !idleTimer.Stop() {
				select {
				case <-idleTimer.C:
				default:
				}
			}
			idleTimer.Reset(120 * time.Second)

			if !ok {
				break
			}
			if msg.Type == "error" {
				b.logger.Printf("[BROWSER BRIDGE] Job %s failed with error: %s", jobID, msg.Error)
				return errors.New(msg.Error)
			}
			if msg.Type == "done" {
				cleanText, toolCalls := ParseToolCalls(fullText, extractToolSchemas(body))
				finishReason := "stop"
				msgMap := map[string]any{
					"role":    "assistant",
					"content": cleanText,
				}
				if len(toolCalls) > 0 {
					b.logger.Printf("[BROWSER BRIDGE] Job %s extracted %d tool call(s) (first: %s), finish_reason=tool_calls", jobID, len(toolCalls), toolCalls[0].Function.Name)
					msgMap["tool_calls"] = toolCalls
					finishReason = "tool_calls"
				}

				respObj := map[string]any{
					"id":      "chatcmpl-" + jobID,
					"object":  "chat.completion",
					"created": createdNow,
					"model":   model,
					"choices": []map[string]any{
						{
							"index":         0,
							"message":       msgMap,
							"finish_reason": finishReason,
						},
					},
					"usage": map[string]int{
						"prompt_tokens":     0,
						"completion_tokens": 0,
						"total_tokens":      0,
					},
				}
				writeJSON(w, http.StatusOK, respObj)
				_ = username
				b.logger.Printf("[BROWSER BRIDGE] Job %s completed successfully (non-stream, len: %d)", jobID, len(fullText))
				return nil
			}
			if msg.Type == "chunk" {
				fullText += msg.Text
			}
		}
	}
}
