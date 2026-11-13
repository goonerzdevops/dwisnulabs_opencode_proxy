package proxy

import (
	"strings"
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

func TestRestrictedAndCollisionToolAliasing(t *testing.T) {
	cfg := config.Default()

	toolDef := func(name string) map[string]any {
		return map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        name,
				"description": "custom " + name,
				"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
			},
		}
	}

	in := map[string]any{
		"model": "glm-5.3-flash-free",
		"messages": []any{
			map[string]any{"role": "user", "content": "hi"},
			map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []any{
					map[string]any{
						"id":   "c1",
						"type": "function",
						"function": map[string]any{
							"name":      "subagent_fork",
							"arguments": "{}",
						},
					},
				},
			},
			map[string]any{"role": "tool", "tool_call_id": "c1", "name": "subagent_fork", "content": "ok"},
		},
		"tool_choice": map[string]any{
			"type":     "function",
			"function": map[string]any{"name": "working_directory"},
		},
		"tools": []any{
			toolDef("delegate_fork"),
			toolDef("subagent_fork"),
			toolDef("working_directory"),
			toolDef("read"),
		},
	}

	prep, err := Prepare(cfg, in)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	// Verify caller's raw slice tools were NOT mutated in-place.
	rawTools := in["tools"].([]any)
	if got := rawTools[1].(map[string]any)["function"].(map[string]any)["name"]; got != "subagent_fork" {
		t.Fatalf("expected raw tool name preserved as subagent_fork, got %v", got)
	}

	prepTools := prep.Body["tools"].([]any)
	var names []string
	for _, raw := range prepTools {
		names = append(names, raw.(map[string]any)["function"].(map[string]any)["name"].(string))
	}

	// Expect delegate_fork untouched, subagent_fork -> delegate_fork_2 (avoiding collision),
	// working_directory -> workspace_dir, read -> hidden_read_1, plus fingerprint read & shell.
	if names[0] != "delegate_fork" || names[1] != "delegate_fork_2" || names[2] != "workspace_dir" || names[3] != "hidden_read_1" {
		t.Fatalf("unexpected prepared tool names: %v", names)
	}
	if got := prep.Renames.Restore("delegate_fork_2"); got != "subagent_fork" {
		t.Errorf("Restore(delegate_fork_2) = %q, want subagent_fork", got)
	}
	if got := prep.Renames.Restore("workspace_dir"); got != "working_directory" {
		t.Errorf("Restore(workspace_dir) = %q, want working_directory", got)
	}
	if got := prep.Renames.Restore("hidden_read_1"); got != "read" {
		t.Errorf("Restore(hidden_read_1) = %q, want read", got)
	}

	choiceFn := prep.Body["tool_choice"].(map[string]any)["function"].(map[string]any)["name"]
	if choiceFn != "workspace_dir" {
		t.Errorf("tool_choice name = %v, want workspace_dir", choiceFn)
	}

	msgs := prep.Body["messages"].([]any)
	var sysPrompt string
	var histCall, toolMsgName any
	for _, raw := range msgs {
		m := raw.(map[string]any)
		if m["role"] == "system" {
			sysPrompt, _ = m["content"].(string)
		}
		if m["role"] == "assistant" {
			histCall = m["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)["name"]
		}
		if m["role"] == "tool" {
			toolMsgName = m["name"]
		}
	}
	for _, want := range []string{
		"not available to you: read, shell",
		`"read" -> "hidden_read_1"`,
		`"subagent_fork" -> "delegate_fork_2"`,
		`"working_directory" -> "workspace_dir"`,
	} {
		if !strings.Contains(sysPrompt, want) {
			t.Errorf("system prompt missing %q:\n%s", want, sysPrompt)
		}
	}
	if histCall != "delegate_fork_2" {
		t.Errorf("assistant history tool_call = %v, want delegate_fork_2", histCall)
	}
	if toolMsgName != "delegate_fork_2" {
		t.Errorf("tool message name = %v, want delegate_fork_2", toolMsgName)
	}

	if !ConcealAllCallerTools(prep) {
		t.Fatalf("expected ConcealAllCallerTools to rename caller tools")
	}
	// Fingerprint read/shell remain verbatim; caller tools become fn_1..fn_4
	var restored []string
	for _, raw := range prep.Body["tools"].([]any) {
		n := raw.(map[string]any)["function"].(map[string]any)["name"].(string)
		restored = append(restored, prep.Renames.Restore(n))
	}
	if restored[1] != "subagent_fork" || restored[2] != "working_directory" || restored[3] != "read" {
		t.Errorf("composed Restore failed, got %v", restored)
	}
	if ConcealAllCallerTools(prep) {
		t.Errorf("second ConcealAllCallerTools should return false when already concealed")
	}
}
