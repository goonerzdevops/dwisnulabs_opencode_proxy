package proxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ResponsesBridge converts an upstream Responses SSE stream into OpenAI Chat
// Completions output, either as streaming chunks or as a single aggregated body.
// It accumulates text and tool calls so a non-streaming client can be served
// from the same stream the free tier forces us to request.
type ResponsesBridge struct {
	model   string
	renames RenameMap
	stream  bool
	w       http.ResponseWriter

	responseID string
	created    int64
	modelEcho  string

	roleSent     bool
	text         strings.Builder
	reasoning    strings.Builder
	toolCalls    []*bridgeToolCall
	toolCallByID map[string]*bridgeToolCall
	sawToolCall  bool
	completed    bool
	usage        map[string]any
	status       string

	errPayload map[string]any
	headerSent bool
}

type bridgeToolCall struct {
	index     int
	id        string
	name      string
	arguments strings.Builder
}

// NewResponsesBridge builds a bridge for model, restoring caller tool names from
// renames. w is nil for non-streaming requests.
func NewResponsesBridge(model string, renames RenameMap, stream bool, w http.ResponseWriter) *ResponsesBridge {
	return &ResponsesBridge{
		model:        model,
		renames:      renames,
		stream:       stream,
		w:            w,
		created:      time.Now().Unix(),
		modelEcho:    model,
		toolCallByID: map[string]*bridgeToolCall{},
	}
}

// Failure returns the upstream error payload, if the stream carried one.
func (b *ResponsesBridge) Failure() map[string]any { return b.errPayload }

// HandleEvent ingests one upstream SSE event.
func (b *ResponsesBridge) HandleEvent(eventName string, data []byte) error {
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("decode upstream event: %w", err)
	}
	eventType := stringField(payload, "type")
	if eventType == "" {
		eventType = eventName
	}

	switch eventType {
	case "response.created", "response.in_progress":
		if response, ok := payload["response"].(map[string]any); ok {
			b.captureResponse(response)
		}
	case "response.output_item.added", "response.output_item.done":
		item, _ := payload["item"].(map[string]any)
		b.handleItem(item, eventType == "response.output_item.done")
	case "response.content_part.added":
		// Content parts are announced before their deltas; nothing to emit.
	case "response.output_text.delta":
		b.appendText(stringField(payload, "delta"))
	case "response.reasoning_summary_text.delta", "response.reasoning_text.delta":
		b.appendReasoning(stringField(payload, "delta"))
	case "response.reasoning_summary_part.added":
	case "response.function_call_arguments.delta":
		b.appendArguments(payload, stringField(payload, "delta"))
	case "response.function_call_arguments.done":
		b.appendArguments(payload, "")
	case "response.completed", "response.incomplete", "response.failed":
		b.completed = true
		b.status = eventType
		if response, ok := payload["response"].(map[string]any); ok {
			if usage, ok := response["usage"].(map[string]any); ok {
				b.usage = usage
			}
			if status := stringField(response, "status"); status != "" {
				b.status = status
			}
		}
	case "error":
		if errPayload, ok := payload["error"].(map[string]any); ok {
			b.errPayload = errPayload
		} else {
			b.errPayload = payload
		}
	default:
		// Unknown/unsupported events (queueing, web search, MCP) are dropped.
	}
	return nil
}

func (b *ResponsesBridge) captureResponse(response map[string]any) {
	if id := stringField(response, "id"); id != "" && b.responseID == "" {
		b.responseID = id
	}
	if model := stringField(response, "model"); model != "" {
		b.modelEcho = model
	}
	if usage, ok := response["usage"].(map[string]any); ok {
		b.usage = usage
	}
}

// handleItem tracks function_call items so streamed argument deltas can be
// attached to the right tool call.
func (b *ResponsesBridge) handleItem(item map[string]any, done bool) {
	if item == nil {
		return
	}
	if kind, _ := item["type"].(string); kind != "function_call" {
		return
	}
	callID := stringField(item, "call_id")
	itemID := stringField(item, "id")
	key := callID
	if key == "" {
		key = itemID
	}

	call, exists := b.toolCallByID[key]
	if !exists {
		call = &bridgeToolCall{
			index: len(b.toolCalls),
			id:    callID,
			name:  b.renames.Restore(stringField(item, "name")),
		}
		if call.id == "" {
			call.id = itemID
		}
		b.toolCalls = append(b.toolCalls, call)
		if key != "" {
			b.toolCallByID[key] = call
		}
		b.sawToolCall = true
		b.emitToolCallStart(call)
	}
	if done {
		if arguments, _ := item["arguments"].(string); arguments != "" && call.arguments.Len() == 0 {
			call.arguments.WriteString(arguments)
		}
	}
}

func (b *ResponsesBridge) appendText(delta string) {
	if delta == "" {
		return
	}
	b.text.WriteString(delta)
	b.emit(b.chunk(map[string]any{
		"delta": map[string]any{"content": delta},
	}))
}

// appendReasoning streams provider reasoning to clients that understand the
// OpenAI-compatible reasoning_content extension, and keeps it for the
// aggregated response body.
func (b *ResponsesBridge) appendReasoning(delta string) {
	if delta == "" {
		return
	}
	b.reasoning.WriteString(delta)
	b.emit(b.chunk(map[string]any{
		"delta": map[string]any{"reasoning_content": delta},
	}))
}

func (b *ResponsesBridge) appendArguments(payload map[string]any, delta string) {
	if delta == "" {
		return
	}
	key := stringField(payload, "call_id")
	if key == "" {
		key = stringField(payload, "item_id")
	}
	call, ok := b.toolCallByID[key]
	if !ok {
		// Argument deltas for an item we never saw: synthesise the tool call so
		// the client still receives a well-formed stream.
		b.handleItem(map[string]any{
			"type":    "function_call",
			"call_id": key,
			"name":    stringField(payload, "name"),
		}, false)
		call, ok = b.toolCallByID[key]
		if !ok {
			return
		}
	}
	call.arguments.WriteString(delta)
	b.emit(b.chunk(map[string]any{
		"delta": map[string]any{
			"tool_calls": []any{map[string]any{
				"index":    call.index,
				"function": map[string]any{"arguments": delta},
			}},
		},
	}))
}

// completionID is stable for the whole request so every chunk carries the same
// chatcmpl id, exactly like OpenAI.
func (b *ResponsesBridge) completionID() string {
	if b.responseID != "" {
		return b.responseID
	}
	return "chatcmpl-" + randomBase62(24)
}

func (b *ResponsesBridge) chunk(delta map[string]any) map[string]any {
	if !b.roleSent {
		delta["role"] = "assistant"
		b.roleSent = true
	}
	return map[string]any{
		"id":      b.completionID(),
		"object":  "chat.completion.chunk",
		"created": b.created,
		"model":   b.modelEcho,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         delta,
			"finish_reason": nil,
		}},
	}
}

func (b *ResponsesBridge) emitToolCallStart(call *bridgeToolCall) {
	b.emit(b.chunk(map[string]any{
		"delta": map[string]any{
			"tool_calls": []any{map[string]any{
				"index":    call.index,
				"id":       call.id,
				"type":     "function",
				"function": map[string]any{"name": call.name, "arguments": ""},
			}},
		},
	}))
}

// emit writes one SSE chunk. Non-streaming requests accumulate silently.
func (b *ResponsesBridge) emit(chunk map[string]any) {
	if !b.stream || b.w == nil {
		return
	}
	b.ensureStreamHeaders()
	blob, err := json.Marshal(chunk)
	if err != nil {
		return
	}
	b.w.Write([]byte("data: "))
	b.w.Write(blob)
	b.w.Write([]byte("\n\n"))
	if flusher, ok := b.w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (b *ResponsesBridge) ensureStreamHeaders() {
	if b.headerSent || b.w == nil {
		return
	}
	b.headerSent = true
	header := b.w.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache, no-transform")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	b.w.WriteHeader(http.StatusOK)
}

// Written reports whether any byte of the client response has been produced.
func (b *ResponsesBridge) Written() bool { return b.headerSent }

// Finish flushes the terminator (streaming) or writes the aggregated body.
func (b *ResponsesBridge) Finish() {
	if b.stream {
		b.finishStream()
		return
	}
	b.finishJSON()
}

func (b *ResponsesBridge) finishStream() {
	if b.w == nil {
		return
	}
	b.ensureStreamHeaders()
	final := b.chunk(map[string]any{})
	final["choices"] = []any{map[string]any{
		"index":         0,
		"delta":         map[string]any{},
		"finish_reason": b.finishReason(),
	}}
	if usage := b.chatUsage(); usage != nil {
		final["usage"] = usage
	}
	blob, err := json.Marshal(final)
	if err == nil {
		b.w.Write([]byte("data: "))
		b.w.Write(blob)
		b.w.Write([]byte("\n\n"))
	}
	b.w.Write([]byte("data: [DONE]\n\n"))
	if flusher, ok := b.w.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (b *ResponsesBridge) finishJSON() {
	if b.w == nil {
		return
	}
	if !b.roleSent {
		b.roleSent = true
	}
	message := map[string]any{
		"role":    "assistant",
		"content": b.text.String(),
	}
	if reasoning := strings.TrimSpace(b.reasoning.String()); reasoning != "" {
		message["reasoning_content"] = reasoning
	}
	if len(b.toolCalls) > 0 {
		calls := make([]any, 0, len(b.toolCalls))
		for _, call := range b.toolCalls {
			arguments := call.arguments.String()
			if arguments == "" {
				arguments = "{}"
			}
			calls = append(calls, map[string]any{
				"index": call.index,
				"id":    call.id,
				"type":  "function",
				"function": map[string]any{
					"name":      call.name,
					"arguments": arguments,
				},
			})
		}
		message["tool_calls"] = calls
	}
	if message["content"] == "" && len(b.toolCalls) == 0 {
		message["content"] = ""
	}

	body := map[string]any{
		"id":      b.completionID(),
		"object":  "chat.completion",
		"created": b.created,
		"model":   b.modelEcho,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": b.finishReason(),
		}},
	}
	if usage := b.chatUsage(); usage != nil {
		body["usage"] = usage
	}

	b.ensureJSONHeaders()
	encoder := json.NewEncoder(b.w)
	encoder.SetEscapeHTML(false)
	encoder.Encode(body)
}

func (b *ResponsesBridge) ensureJSONHeaders() {
	if b.headerSent || b.w == nil {
		return
	}
	b.headerSent = true
	b.w.Header().Set("Content-Type", "application/json; charset=utf-8")
	b.w.WriteHeader(http.StatusOK)
}

func (b *ResponsesBridge) finishReason() string {
	if b.sawToolCall {
		return "tool_calls"
	}
	if b.status == "incomplete" {
		return "length"
	}
	return "stop"
}

// chatUsage maps Responses token accounting onto Chat Completions fields.
func (b *ResponsesBridge) chatUsage() map[string]any {
	if len(b.usage) == 0 {
		return nil
	}
	prompt := int(numberField(b.usage, "input_tokens"))
	completion := int(numberField(b.usage, "output_tokens"))
	total := int(numberField(b.usage, "total_tokens"))
	if total == 0 {
		total = prompt + completion
	}
	usage := map[string]any{
		"prompt_tokens":     prompt,
		"completion_tokens": completion,
		"total_tokens":      total,
	}
	if promptDetails, ok := b.usage["input_tokens_details"].(map[string]any); ok {
		usage["prompt_tokens_details"] = map[string]any{
			"cached_tokens": int(numberField(promptDetails, "cached_tokens")),
		}
	}
	if completionDetails, ok := b.usage["output_tokens_details"].(map[string]any); ok {
		usage["completion_tokens_details"] = map[string]any{
			"reasoning_tokens": int(numberField(completionDetails, "reasoning_tokens")),
		}
	}
	return usage
}

// ActualModel returns the resolved model echoed by upstream, or fallback to requested model.
func (b *ResponsesBridge) ActualModel() string {
	if b.modelEcho != "" {
		return b.modelEcho
	}
	return b.model
}

// UsageTokens returns prompt tokens, cached prompt tokens, and completion tokens.
func (b *ResponsesBridge) UsageTokens() (prompt, cached, output int) {
	if len(b.usage) == 0 {
		outWords := len(strings.Fields(b.text.String())) + len(strings.Fields(b.reasoning.String()))
		return 0, 0, outWords
	}
	prompt = int(numberField(b.usage, "input_tokens"))
	output = int(numberField(b.usage, "output_tokens"))
	if promptDetails, ok := b.usage["input_tokens_details"].(map[string]any); ok {
		cached = int(numberField(promptDetails, "cached_tokens"))
	}
	return prompt, cached, output
}

