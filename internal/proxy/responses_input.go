package proxy

import (
	"encoding/json"
	"fmt"
	"strings"
)

// messagesToResponsesInput converts a Chat Completions history into Responses
// input items. System/developer text is hoisted into `instructions`, which is the
// Responses API equivalent of the leading system prompt.
func messagesToResponsesInput(messages []any) ([]any, string, error) {
	items := make([]any, 0, len(messages))
	var systemParts []string

	for index, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			return nil, "", fmt.Errorf("messages[%d] must be an object", index)
		}
		role, _ := message["role"].(string)

		switch role {
		case "system", "developer":
			if text := messageText(message["content"]); text != "" {
				systemParts = append(systemParts, text)
			}
		case "user":
			parts := responsesContentParts(message["content"], "input")
			if len(parts) == 0 {
				continue
			}
			items = append(items, map[string]any{
				"type":    "message",
				"role":    "user",
				"content": parts,
			})
		case "assistant":
			if parts := responsesContentParts(message["content"], "output"); len(parts) > 0 {
				items = append(items, map[string]any{
					"type":    "message",
					"role":    "assistant",
					"content": parts,
				})
			}
			for _, rawCall := range toolCalls(message) {
				name, _ := rawCall["name"].(string)
				if name == "" {
					continue
				}
				items = append(items, map[string]any{
					"type":      "function_call",
					"call_id":   clampCallID(stringField(rawCall, "id")),
					"name":      name,
					"arguments": stringField(rawCall, "arguments"),
				})
			}
		case "tool", "function":
			items = append(items, map[string]any{
				"type":    "function_call_output",
				"call_id": clampCallID(rawCallID(message)),
				"output":  messageText(message["content"]),
			})
		default:
			return nil, "", fmt.Errorf("messages[%d] has unsupported role %q", index, role)
		}
	}
	return items, strings.Join(systemParts, "\n\n"), nil
}

// toolCalls extracts assistant tool calls in a normalised shape.
func toolCalls(message map[string]any) []map[string]any {
	raw, ok := message["tool_calls"].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		call, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := call["function"].(map[string]any)
		if !ok {
			continue
		}
		out = append(out, map[string]any{
			"id":        stringField(call, "id"),
			"name":      stringField(fn, "name"),
			"arguments": marshalJSON(fn["arguments"]),
		})
	}
	return out
}

// rawCallID resolves the call id a tool result must echo back. Responses items
// use call_id, Chat messages use tool_call_id.
func rawCallID(message map[string]any) string {
	if id := stringField(message, "tool_call_id"); id != "" {
		return id
	}
	return stringField(message, "call_id")
}

// responsesContentParts converts Chat content into Responses content parts.
// kind is "input" for user turns and "output" for assistant turns.
func responsesContentParts(content any, kind string) []any {
	textType := "input_text"
	if kind == "output" {
		textType = "output_text"
	}

	switch value := content.(type) {
	case string:
		if strings.TrimSpace(value) == "" {
			return nil
		}
		return []any{map[string]any{"type": textType, "text": value}}
	case []any:
		parts := make([]any, 0, len(value))
		for _, raw := range value {
			part, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			switch partType, _ := part["type"].(string); partType {
			case "text", "input_text", "output_text":
				if text, _ := part["text"].(string); text != "" {
					parts = append(parts, map[string]any{"type": textType, "text": text})
				}
			case "image_url":
				url := ""
				switch image := part["image_url"].(type) {
				case string:
					url = image
				case map[string]any:
					url, _ = image["url"].(string)
				}
				if url != "" {
					parts = append(parts, map[string]any{"type": "input_image", "image_url": url})
				}
			case "input_image":
				parts = append(parts, part)
			}
		}
		return parts
	default:
		return nil
	}
}

// messageText flattens any content shape into plain text.
func messageText(content any) string {
	switch value := content.(type) {
	case string:
		return value
	case []any:
		var builder strings.Builder
		for _, raw := range value {
			part, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			text, _ := part["text"].(string)
			if text == "" {
				continue
			}
			if builder.Len() > 0 {
				builder.WriteString("\n")
			}
			builder.WriteString(text)
		}
		return builder.String()
	case map[string]any:
		text, _ := value["text"].(string)
		return text
	case json.RawMessage:
		return string(value)
	default:
		return ""
	}
}

// normalizeResponsesItems sanitises items a client already sent in Responses
// shape: call ids are clamped and arguments/outputs are coerced to JSON strings.
func normalizeResponsesItems(items []any) ([]any, error) {
	out := make([]any, 0, len(items))
	for index, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("input[%d] must be an object", index)
		}
		switch itemType, _ := item["type"].(string); itemType {
		case "function_call":
			item["call_id"] = clampCallID(stringField(item, "call_id"))
			item["arguments"] = marshalJSON(item["arguments"])
		case "function_call_output":
			item["call_id"] = clampCallID(stringField(item, "call_id"))
			item["output"] = marshalJSON(item["output"])
		case "", "message":
			role, _ := item["role"].(string)
			kind := "input"
			if role == "assistant" {
				kind = "output"
			}
			if parts := responsesContentParts(item["content"], kind); len(parts) > 0 {
				item["type"] = "message"
				item["content"] = parts
			}
		}
		out = append(out, item)
	}
	return out, nil
}
