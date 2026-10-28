package proxy

import (
	"testing"

	"opencode-proxy/internal/config"
)

func TestEnsureReasoningContentOnAssistantMessages(t *testing.T) {
	cfg := config.Default()

	in := map[string]any{
		"model": "deepseek-v4.1-flash-free",
		"messages": []any{
			map[string]any{
				"role":    "user",
				"content": "Please read server.js",
			},
			map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_1",
						"type": "function",
						"function": map[string]any{
							"name":      "read",
							"arguments": `{"path":"server.js"}`,
						},
					},
				},
			},
			map[string]any{
				"role":         "tool",
				"tool_call_id": "call_1",
				"content":      "file contents here",
			},
		},
	}

	prep, err := Prepare(cfg, in)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	rawMsgs, ok := prep.Body["messages"].([]any)
	if !ok {
		t.Fatalf("messages is not []any")
	}

	var foundAssistant bool
	for _, m := range rawMsgs {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if msg["role"] == "assistant" {
			foundAssistant = true
			rc, exists := msg["reasoning_content"]
			if !exists {
				t.Errorf("expected reasoning_content to exist on assistant message")
			}
			if rc != "" {
				t.Errorf("expected reasoning_content to be empty string, got %v", rc)
			}
		}
	}

	if !foundAssistant {
		t.Errorf("assistant message not found in prepared body")
	}
}
