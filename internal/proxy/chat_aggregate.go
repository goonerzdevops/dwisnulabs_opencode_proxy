package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ChatAggregator turns an upstream Chat Completions SSE stream into one
// non-streaming chat.completion body. The free tier always streams, so a client
// asking for `stream:false` is served from the accumulated deltas.
type ChatAggregator struct {
	model   string
	renames RenameMap

	id           string
	created      int64
	modelEcho    string
	role         string
	content      strings.Builder
	reasoning    strings.Builder
	toolCalls    []*bridgeToolCall
	toolCallByIx map[int]*bridgeToolCall
	finishReason string
	usage        map[string]any
	errPayload   map[string]any
}

// NewChatAggregator builds an aggregator for the requested model.
func NewChatAggregator(model string, renames RenameMap) *ChatAggregator {
	return &ChatAggregator{
		model:        model,
		renames:      renames,
		created:      time.Now().Unix(),
		modelEcho:    model,
		role:         "assistant",
		toolCallByIx: map[int]*bridgeToolCall{},
	}
}

// Failure returns the upstream error payload when the stream carried one.
func (a *ChatAggregator) Failure() map[string]any { return a.errPayload }

// HandleData ingests one `data:` payload of the upstream chat SSE stream.
func (a *ChatAggregator) HandleData(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "[DONE]" {
		return nil
	}
	var chunk map[string]any
	if err := json.Unmarshal(data, &chunk); err != nil {
		return fmt.Errorf("decode upstream chunk: %w", err)
	}
	if errPayload, ok := chunk["error"].(map[string]any); ok {
		a.errPayload = errPayload
		return nil
	}
	if id := stringField(chunk, "id"); id != "" && a.id == "" {
		a.id = id
	}
	if model := stringField(chunk, "model"); model != "" {
		a.modelEcho = model
	}
	if created := int(numberField(chunk, "created")); created > 0 {
		a.created = int64(created)
	}
	if usage, ok := chunk["usage"].(map[string]any); ok && len(usage) > 0 {
		a.usage = usage
	}

	choices, ok := chunk["choices"].([]any)
	if !ok {
		return nil
	}
	for _, raw := range choices {
		choice, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if reason := stringField(choice, "finish_reason"); reason != "" {
			a.finishReason = reason
		}
		delta, ok := choice["delta"].(map[string]any)
		if !ok {
			continue
		}
		if role := stringField(delta, "role"); role != "" {
			a.role = role
		}
		if content := stringField(delta, "content"); content != "" {
			a.content.WriteString(content)
		}
		if reasoning := stringField(delta, "reasoning_content"); reasoning != "" {
			a.reasoning.WriteString(reasoning)
		}
		calls, ok := delta["tool_calls"].([]any)
		if !ok {
			continue
		}
		for _, rawCall := range calls {
			call, ok := rawCall.(map[string]any)
			if !ok {
				continue
			}
			a.mergeToolCall(call)
		}
	}
	return nil
}

func (a *ChatAggregator) mergeToolCall(call map[string]any) {
	index := int(numberField(call, "index"))
	entry, exists := a.toolCallByIx[index]
	if !exists {
		entry = &bridgeToolCall{index: index}
		a.toolCalls = append(a.toolCalls, entry)
		a.toolCallByIx[index] = entry
	}
	if id := stringField(call, "id"); id != "" {
		entry.id = id
	}
	if function, ok := call["function"].(map[string]any); ok {
		if name := stringField(function, "name"); name != "" {
			entry.name = a.renames.Restore(name)
		}
		if arguments := stringField(function, "arguments"); arguments != "" {
			entry.arguments.WriteString(arguments)
		}
	}
}

// Result renders the aggregated chat.completion body.
func (a *ChatAggregator) Result() map[string]any {
	message := map[string]any{
		"role":    a.role,
		"content": a.content.String(),
	}
	if reasoning := strings.TrimSpace(a.reasoning.String()); reasoning != "" {
		message["reasoning_content"] = reasoning
	}
	if len(a.toolCalls) > 0 {
		calls := make([]any, 0, len(a.toolCalls))
		for _, call := range a.toolCalls {
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

	id := a.id
	if id == "" {
		id = "chatcmpl-" + randomBase62(24)
	}
	finish := a.finishReason
	if finish == "" {
		if len(a.toolCalls) > 0 {
			finish = "tool_calls"
		} else {
			finish = "stop"
		}
	}
	body := map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": a.created,
		"model":   a.modelEcho,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": finish,
		}},
	}
	if len(a.usage) > 0 {
		body["usage"] = a.usage
	}
	return body
}

// ActualModel returns the echoed model or requested model.
func (a *ChatAggregator) ActualModel() string {
	if a.modelEcho != "" {
		return a.modelEcho
	}
	return a.model
}

// UsageTokens returns prompt tokens, cached prompt tokens, and completion tokens.
func (a *ChatAggregator) UsageTokens() (prompt, cached, output int) {
	if len(a.usage) == 0 {
		outWords := len(strings.Fields(a.content.String())) + len(strings.Fields(a.reasoning.String()))
		return 0, 0, outWords
	}
	prompt = int(numberField(a.usage, "prompt_tokens"))
	output = int(numberField(a.usage, "completion_tokens"))
	if promptDetails, ok := a.usage["prompt_tokens_details"].(map[string]any); ok {
		cached = int(numberField(promptDetails, "cached_tokens"))
	}
	return prompt, cached, output
}

