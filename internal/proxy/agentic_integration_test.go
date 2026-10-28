package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"opencode-proxy/internal/config"
)

// generateLargeText generates realistic large context text (e.g. simulated code / logs).
func generateLargeText(prefix string, lineCount int) string {
	var sb strings.Builder
	sb.WriteString(prefix)
	sb.WriteString("\n")
	for i := 1; i <= lineCount; i++ {
		sb.WriteString(fmt.Sprintf("// Line %04d: func ProcessStep%04d(ctx context.Context, payload []byte) (Result, error) { return execute(payload) }\n", i, i))
	}
	return sb.String()
}

// TestAgenticMultiTurnLargeContextIntegration simulates an Agentic AI (like Cline in VS Code)
// executing a 3-turn workflow with large system prompts, multiple tools, large command outputs,
// and session continuity.
func TestAgenticMultiTurnLargeContextIntegration(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.json")
	mgr, err := config.NewManager(cfgPath)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	server := NewServer(mgr, nil)
	defer server.Close()

	// Intercept upstream traffic to verify wire contract and supply streaming responses.
	var (
		mu           sync.Mutex
		turnCount    int
		turnSessions []string
		turnPaths    []string
		turnBodies   []map[string]any
	)

	server.upstream.pool.mu.Lock()
	server.upstream.pool.nodes[0].tlsClient = nil
	server.upstream.pool.nodes[0].client = &http.Client{
		Transport: &mockRoundTripper{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				mu.Lock()
				turnCount++
				currentTurn := turnCount
				sessionID := req.Header.Get("x-opencode-session")
				turnSessions = append(turnSessions, sessionID)
				turnPaths = append(turnPaths, req.URL.Path)

				// Verify headers contract required by OpenCode gate
				if req.Header.Get("User-Agent") != UpstreamUserAgent {
					t.Errorf("turn %d: User-Agent = %q, want %q", currentTurn, req.Header.Get("User-Agent"), UpstreamUserAgent)
				}
				if len(req.Header.Get("x-opencode-project")) != 40 {
					t.Errorf("turn %d: x-opencode-project %q must be 40 hex chars", currentTurn, req.Header.Get("x-opencode-project"))
				}
				if req.Header.Get("b3") == "" || req.Header.Get("traceparent") == "" {
					t.Errorf("turn %d: missing b3 or traceparent headers", currentTurn)
				}

				// Read and decode body
				bodyBytes, _ := io.ReadAll(req.Body)
				var bodyMap map[string]any
				_ = json.Unmarshal(bodyBytes, &bodyMap)
				turnBodies = append(turnBodies, bodyMap)
				mu.Unlock()

				rec := httptest.NewRecorder()
				rec.Header().Set("Content-Type", "text/event-stream")
				rec.WriteHeader(http.StatusOK)

				switch currentTurn {
				case 1:
					// Model outputs reasoning and calls tool execute_command
					rec.WriteString("data: {\"id\":\"chatcmpl-t1\",\"object\":\"chat.completion.chunk\",\"created\":1730000000,\"model\":\"big-pickle\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"Inspecting workspace...\"},\"finish_reason\":null}]}\n\n")
					rec.WriteString("data: {\"id\":\"chatcmpl-t1\",\"object\":\"chat.completion.chunk\",\"created\":1730000000,\"model\":\"big-pickle\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_exec_001\",\"type\":\"function\",\"function\":{\"name\":\"execute_command\",\"arguments\":\"{\\\"command\\\":\\\"go test ./...\\\"}\"}}]},\"finish_reason\":null}]}\n\n")
					rec.WriteString("data: {\"id\":\"chatcmpl-t1\",\"object\":\"chat.completion.chunk\",\"created\":1730000000,\"model\":\"big-pickle\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
					rec.WriteString("data: [DONE]\n\n")
				case 2:
					// Model outputs reasoning and calls tool read_file
					rec.WriteString("data: {\"id\":\"chatcmpl-t2\",\"object\":\"chat.completion.chunk\",\"created\":1730000010,\"model\":\"big-pickle\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"Tests passed. Reading code file...\"},\"finish_reason\":null}]}\n\n")
					rec.WriteString("data: {\"id\":\"chatcmpl-t2\",\"object\":\"chat.completion.chunk\",\"created\":1730000010,\"model\":\"big-pickle\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_read_002\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"internal/proxy/transport.go\\\"}\"}}]},\"finish_reason\":null}]}\n\n")
					rec.WriteString("data: {\"id\":\"chatcmpl-t2\",\"object\":\"chat.completion.chunk\",\"created\":1730000010,\"model\":\"big-pickle\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
					rec.WriteString("data: [DONE]\n\n")
				case 3:
					// Model provides final answer
					rec.WriteString("data: {\"id\":\"chatcmpl-t3\",\"object\":\"chat.completion.chunk\",\"created\":1730000020,\"model\":\"big-pickle\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Refactoring complete: transport.go verified with BoringSSL JA4 fingerprint.\"},\"finish_reason\":null}]}\n\n")
					rec.WriteString("data: {\"id\":\"chatcmpl-t3\",\"object\":\"chat.completion.chunk\",\"created\":1730000020,\"model\":\"big-pickle\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
					rec.WriteString("data: [DONE]\n\n")
				}
				return rec.Result(), nil
			},
		},
	}
	server.upstream.pool.mu.Unlock()

	// Cline tool declarations
	clineTools := []map[string]any{
		{
			"type": "function",
			"function": map[string]any{
				"name":        "execute_command",
				"description": "Execute a CLI command on the system",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"command": map[string]any{"type": "string"}},
					"required":   []string{"command"},
				},
			},
		},
		{
			"type": "function",
			"function": map[string]any{
				"name":        "read_file",
				"description": "Read file contents from filesystem",
				"parameters": map[string]any{
					"type":       "object",
					"properties": map[string]any{"path": map[string]any{"type": "string"}},
					"required":   []string{"path"},
				},
			},
		},
	}

	// -------------------------------------------------------------
	// TURN 1: Initial Prompt with Large System Instructions (~15KB)
	// -------------------------------------------------------------
	largeSystemPrompt := generateLargeText("SYSTEM INSTRUCTIONS: You are Cline, an elite agentic software engineer.\nProject Context & Architecture rules follow:", 150)
	userPrompt1 := "Analyze the test suite and verify if all unit tests in ./... are passing."

	turn1Payload := map[string]any{
		"model":  "big-pickle",
		"stream": true,
		"messages": []any{
			map[string]any{"role": "system", "content": largeSystemPrompt},
			map[string]any{"role": "user", "content": userPrompt1},
		},
		"tools": clineTools,
	}

	resp1, err := sendClientRequest(server.Handler(), turn1Payload)
	if err != nil {
		t.Fatalf("Turn 1 request failed: %v", err)
	}

	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("Turn 1 status = %d, want 200", resp1.StatusCode)
	}

	var turn1ToolCalls []map[string]any
	var turn1Reasoning string
	err = ScanSSE(resp1.Body, func(ev SSEEvent) (bool, error) {
		var chunk map[string]any
		if err := json.Unmarshal(ev.Data, &chunk); err == nil {
			if choices, ok := chunk["choices"].([]any); ok && len(choices) > 0 {
				choice := choices[0].(map[string]any)
				if delta, ok := choice["delta"].(map[string]any); ok {
					if r, ok := delta["reasoning_content"].(string); ok {
						turn1Reasoning += r
					}
					if tc, ok := delta["tool_calls"].([]any); ok {
						for _, call := range tc {
							turn1ToolCalls = append(turn1ToolCalls, call.(map[string]any))
						}
					}
				}
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("Turn 1 SSE scan error: %v", err)
	}

	if len(turn1ToolCalls) == 0 {
		t.Fatalf("Turn 1 expected tool_calls, got 0")
	}
	if !strings.Contains(turn1Reasoning, "Inspecting") {
		t.Errorf("Turn 1 reasoning missing expected text, got %q", turn1Reasoning)
	}

	// Verify upstream received Turn 1 with correct path & injected tools
	mu.Lock()
	if turnCount != 1 {
		t.Fatalf("expected 1 turn upstream, got %d", turnCount)
	}
	if turnPaths[0] != "/inference/openai/v1/chat/completions" {
		t.Errorf("Turn 1 path = %q, want /inference/openai/v1/chat/completions", turnPaths[0])
	}
	turn1Session := turnSessions[0]
	if !SessionPattern.MatchString(turn1Session) {
		t.Errorf("Turn 1 session = %q does not match SessionPattern", turn1Session)
	}
	// Verify that upstream body contains both cline tools and required fingerprint tools
	turn1BodyTools, _ := turnBodies[0]["tools"].([]any)
	hasExecuteCmd, hasFingerprintRead, hasFingerprintShell := false, false, false
	for _, toolRaw := range turn1BodyTools {
		tool := toolRaw.(map[string]any)
		fn := tool["function"].(map[string]any)
		name := fn["name"].(string)
		if name == "execute_command" {
			hasExecuteCmd = true
		}
		if name == "read" {
			hasFingerprintRead = true
		}
		if name == "shell" {
			hasFingerprintShell = true
		}
	}
	if !hasExecuteCmd || !hasFingerprintRead || !hasFingerprintShell {
		t.Errorf("Turn 1 tools missing expected set (exec:%t, read:%t, shell:%t)", hasExecuteCmd, hasFingerprintRead, hasFingerprintShell)
	}
	mu.Unlock()

	// -------------------------------------------------------------
	// TURN 2: Follow-up with Large Tool Output (~20KB)
	// -------------------------------------------------------------
	largeBuildOutput := generateLargeText("=== RUN TestProxyPoolAntiAbuse\nBuild & Execution Log:", 200)

	turn2Payload := map[string]any{
		"model":  "big-pickle",
		"stream": true,
		"messages": []any{
			map[string]any{"role": "system", "content": largeSystemPrompt},
			map[string]any{"role": "user", "content": userPrompt1},
			map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_exec_001",
						"type": "function",
						"function": map[string]any{
							"name":      "execute_command",
							"arguments": `{"command":"go test ./..."}`,
						},
					},
				},
			},
			map[string]any{
				"role":         "tool",
				"tool_call_id": "call_exec_001",
				"content":      largeBuildOutput,
			},
		},
		"tools": clineTools,
	}

	resp2, err := sendClientRequest(server.Handler(), turn2Payload)
	if err != nil {
		t.Fatalf("Turn 2 request failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("Turn 2 status = %d, want 200", resp2.StatusCode)
	}

	var turn2ToolCalls []map[string]any
	_ = ScanSSE(resp2.Body, func(ev SSEEvent) (bool, error) {
		var chunk map[string]any
		if err := json.Unmarshal(ev.Data, &chunk); err == nil {
			if choices, ok := chunk["choices"].([]any); ok && len(choices) > 0 {
				choice := choices[0].(map[string]any)
				if delta, ok := choice["delta"].(map[string]any); ok {
					if tc, ok := delta["tool_calls"].([]any); ok {
						for _, call := range tc {
							turn2ToolCalls = append(turn2ToolCalls, call.(map[string]any))
						}
					}
				}
			}
		}
		return false, nil
	})

	if len(turn2ToolCalls) == 0 {
		t.Fatalf("Turn 2 expected next tool call (read_file), got 0")
	}

	// Verify session continuity between Turn 1 and Turn 2
	mu.Lock()
	if turnCount != 2 {
		t.Fatalf("expected 2 turns upstream, got %d", turnCount)
	}
	turn2Session := turnSessions[1]
	if turn2Session != turn1Session {
		t.Errorf("Session continuity failed: Turn 1 session = %q, Turn 2 session = %q", turn1Session, turn2Session)
	}
	// Verify that Turn 2 assistant message in upstream body has reasoning_content populated
	turn2Msgs := turnBodies[1]["messages"].([]any)
	assistantMsg := turn2Msgs[2].(map[string]any)
	if _, ok := assistantMsg["reasoning_content"]; !ok {
		t.Errorf("Turn 2 assistant message missing reasoning_content key required by thinking models")
	}
	mu.Unlock()

	// -------------------------------------------------------------
	// TURN 3: Follow-up with Large Code File Output (~25KB) & Final Answer
	// -------------------------------------------------------------
	largeCodeFile := generateLargeText("package transport\n// Implementation of BoringSSL TLS dialer\n", 250)

	turn3Payload := map[string]any{
		"model":  "big-pickle",
		"stream": true,
		"messages": []any{
			map[string]any{"role": "system", "content": largeSystemPrompt},
			map[string]any{"role": "user", "content": userPrompt1},
			map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_exec_001",
						"type": "function",
						"function": map[string]any{
							"name":      "execute_command",
							"arguments": `{"command":"go test ./..."}`,
						},
					},
				},
			},
			map[string]any{
				"role":         "tool",
				"tool_call_id": "call_exec_001",
				"content":      largeBuildOutput,
			},
			map[string]any{
				"role":    "assistant",
				"content": "",
				"tool_calls": []any{
					map[string]any{
						"id":   "call_read_002",
						"type": "function",
						"function": map[string]any{
							"name":      "read_file",
							"arguments": `{"path":"internal/proxy/transport.go"}`,
						},
					},
				},
			},
			map[string]any{
				"role":         "tool",
				"tool_call_id": "call_read_002",
				"content":      largeCodeFile,
			},
		},
		"tools": clineTools,
	}

	resp3, err := sendClientRequest(server.Handler(), turn3Payload)
	if err != nil {
		t.Fatalf("Turn 3 request failed: %v", err)
	}
	defer resp3.Body.Close()

	if resp3.StatusCode != http.StatusOK {
		t.Fatalf("Turn 3 status = %d, want 200", resp3.StatusCode)
	}

	var turn3Content string
	var finalFinishReason string
	_ = ScanSSE(resp3.Body, func(ev SSEEvent) (bool, error) {
		var chunk map[string]any
		if err := json.Unmarshal(ev.Data, &chunk); err == nil {
			if choices, ok := chunk["choices"].([]any); ok && len(choices) > 0 {
				choice := choices[0].(map[string]any)
				if delta, ok := choice["delta"].(map[string]any); ok {
					if c, ok := delta["content"].(string); ok {
						turn3Content += c
					}
				}
				if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
					finalFinishReason = fr
				}
			}
		}
		return false, nil
	})

	if !strings.Contains(turn3Content, "Refactoring complete") {
		t.Errorf("Turn 3 final content missing expected resolution, got %q", turn3Content)
	}
	if finalFinishReason != "stop" {
		t.Errorf("Turn 3 finish_reason = %q, want stop", finalFinishReason)
	}

	// Verify session continuity across all 3 turns
	mu.Lock()
	if turnCount != 3 {
		t.Fatalf("expected 3 turns upstream, got %d", turnCount)
	}
	if turnSessions[2] != turn1Session {
		t.Errorf("Turn 3 session %q did not match Turn 1 session %q", turnSessions[2], turn1Session)
	}
	mu.Unlock()
}

// TestAgenticToolCollisionWithReadAndShell verifies that if an agentic harness declares
// custom tools that share names with the required fingerprint tools ("read", "shell"),
// the proxy aliases them upstream and transparently restores them to the client.
func TestAgenticToolCollisionWithReadAndShell(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.json")
	mgr, _ := config.NewManager(cfgPath)
	server := NewServer(mgr, nil)
	defer server.Close()

	var upstreamSentTools []any

	server.upstream.pool.mu.Lock()
	server.upstream.pool.nodes[0].tlsClient = nil
	server.upstream.pool.nodes[0].client = &http.Client{
		Transport: &mockRoundTripper{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				bodyBytes, _ := io.ReadAll(req.Body)
				var bodyMap map[string]any
				_ = json.Unmarshal(bodyBytes, &bodyMap)
				upstreamSentTools = bodyMap["tools"].([]any)

				rec := httptest.NewRecorder()
				rec.Header().Set("Content-Type", "text/event-stream")
				rec.WriteHeader(http.StatusOK)

				// Upstream calls the aliased tool read_1
				rec.WriteString("data: {\"id\":\"chatcmpl-col\",\"object\":\"chat.completion.chunk\",\"created\":1730000000,\"model\":\"big-pickle\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_col_001\",\"type\":\"function\",\"function\":{\"name\":\"hidden_read_1\",\"arguments\":\"{\\\"file_path\\\":\\\"main.go\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\n")
				rec.WriteString("data: [DONE]\n\n")
				return rec.Result(), nil
			},
		},
	}
	server.upstream.pool.mu.Unlock()

	// Client declares custom tool named "read" with non-verbatim schema (file_path instead of path)
	customReadTool := map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        "read",
			"description": "Custom agent read tool",
			"parameters": map[string]any{
				"type":       "object",
				"properties": map[string]any{"file_path": map[string]any{"type": "string"}},
				"required":   []string{"file_path"},
			},
		},
	}

	payload := map[string]any{
		"model":  "big-pickle",
		"stream": true,
		"messages": []any{
			map[string]any{"role": "user", "content": "read main.go"},
		},
		"tools": []any{customReadTool},
	}

	resp, err := sendClientRequest(server.Handler(), payload)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var clientReceivedToolName string
	_ = ScanSSE(resp.Body, func(ev SSEEvent) (bool, error) {
		var chunk map[string]any
		if err := json.Unmarshal(ev.Data, &chunk); err == nil {
			if choices, ok := chunk["choices"].([]any); ok && len(choices) > 0 {
				choice := choices[0].(map[string]any)
				if delta, ok := choice["delta"].(map[string]any); ok {
					if tc, ok := delta["tool_calls"].([]any); ok && len(tc) > 0 {
						call := tc[0].(map[string]any)
						fn := call["function"].(map[string]any)
						clientReceivedToolName = fn["name"].(string)
					}
				}
			}
		}
		return false, nil
	})

	// 1. Verify upstream saw the aliased name read_1 alongside verbatim fingerprint read
	var hasHiddenRead1, hasVerbatimRead bool
	for _, raw := range upstreamSentTools {
		tool := raw.(map[string]any)
		fn := tool["function"].(map[string]any)
		if fn["name"] == "hidden_read_1" {
			hasHiddenRead1 = true
		}
		if fn["name"] == "read" {
			hasVerbatimRead = true
		}
	}
	if !hasHiddenRead1 || !hasVerbatimRead {
		t.Errorf("upstream tools mismatch: hasHiddenRead1=%t, hasVerbatimRead=%t", hasHiddenRead1, hasVerbatimRead)
	}

	// 2. Verify client got back the original un-aliased name "read"
	if clientReceivedToolName != "read" {
		t.Errorf("client received tool name = %q, want restored original 'read'", clientReceivedToolName)
	}
}

// TestAgenticNonStreamingLargeContext verifies that non-streaming requests (stream: false)
// work accurately with multi-turn large context via the aggregator.
func TestAgenticNonStreamingLargeContext(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.json")
	mgr, _ := config.NewManager(cfgPath)
	server := NewServer(mgr, nil)
	defer server.Close()

	server.upstream.pool.mu.Lock()
	server.upstream.pool.nodes[0].tlsClient = nil
	server.upstream.pool.nodes[0].client = &http.Client{
		Transport: &mockRoundTripper{
			roundTrip: func(req *http.Request) (*http.Response, error) {
				// Upstream must always see stream: true
				bodyBytes, _ := io.ReadAll(req.Body)
				var bodyMap map[string]any
				_ = json.Unmarshal(bodyBytes, &bodyMap)
				if bodyMap["stream"] != true {
					t.Errorf("upstream expected stream: true, got %v", bodyMap["stream"])
				}

				rec := httptest.NewRecorder()
				rec.Header().Set("Content-Type", "text/event-stream")
				rec.WriteHeader(http.StatusOK)

				rec.WriteString("data: {\"id\":\"chatcmpl-agg\",\"object\":\"chat.completion.chunk\",\"created\":1730000000,\"model\":\"big-pickle\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"content\":\"Aggregated response for large context agent.\"},\"finish_reason\":null}]}\n\n")
				rec.WriteString("data: {\"id\":\"chatcmpl-agg\",\"object\":\"chat.completion.chunk\",\"created\":1730000000,\"model\":\"big-pickle\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"stop\"}]}\n\n")
				rec.WriteString("data: [DONE]\n\n")
				return rec.Result(), nil
			},
		},
	}
	server.upstream.pool.mu.Unlock()

	largeContext := generateLargeText("Reviewing security architecture:\n", 120)
	payload := map[string]any{
		"model":  "big-pickle",
		"stream": false, // client requests non-streaming JSON
		"messages": []any{
			map[string]any{"role": "user", "content": largeContext},
		},
	}

	resp, err := sendClientRequest(server.Handler(), payload)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var jsonResp map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&jsonResp); err != nil {
		t.Fatalf("json decode failed: %v", err)
	}

	choices := jsonResp["choices"].([]any)
	choice := choices[0].(map[string]any)
	msg := choice["message"].(map[string]any)
	content := msg["content"].(string)

	if !strings.Contains(content, "Aggregated response") {
		t.Errorf("content = %q, want 'Aggregated response'", content)
	}
}

// TestClinePromptWithEnvTagRegression ensures that when an agent like Cline injects
// its own <env> block, ApplyEnvFingerprint safely neutralizes it and prepends the authentic
// OpenCode CLI environment block, preventing 429 FreeUsageLimitError.
func TestClinePromptWithEnvTagRegression(t *testing.T) {
	clineSystemPrompt := `You are Cline, an AI coding agent.
Environment you are running in:
<env>
1. Platform: win32
2. Date: 10/28/2026
3. IDE: VS Code
4. Working Directory: c:\D\Project\Mix\opencode_proxy
</env>
Always follow instructions.`

	body := map[string]any{
		"model":  "glm-5.3-flash-free",
		"stream": true,
		"messages": []any{
			map[string]any{"role": "system", "content": clineSystemPrompt},
			map[string]any{"role": "user", "content": "hi"},
		},
	}

	cfg := config.Default()
	prep, err := Prepare(cfg, body)
	if err != nil {
		t.Fatalf("Prepare failed: %v", err)
	}

	prepMsgs := prep.Body["messages"].([]any)
	sysMsg := prepMsgs[0].(map[string]any)
	content := sysMsg["content"].(string)

	// 1. Must contain OpenCode CLI envHeading
	if !strings.Contains(content, "Here is some useful information about the environment you are running in:") {
		t.Errorf("missing OpenCode CLI envHeading in prepared prompt")
	}

	// 2. The FIRST <env> tag in the prompt must be OpenCode's, not Cline's
	firstEnvIdx := strings.Index(content, "<env>")
	firstHeadingIdx := strings.Index(content, "Here is some useful information about the environment you are running in:")
	if firstEnvIdx == -1 || firstHeadingIdx == -1 || firstEnvIdx < firstHeadingIdx {
		t.Errorf("expected OpenCode env block to precede <env>, got firstEnvIdx=%d, firstHeadingIdx=%d", firstEnvIdx, firstHeadingIdx)
	}

	// 3. Cline's original <env> must be neutralized to <environment_info>
	if strings.Contains(content, "<cline_env>") || strings.Count(content, "<env>") > 1 {
		t.Errorf("expected exactly 1 authentic <env> block, got: %s", content)
	}
}

// TestLiveAgenticMultiTurnUpstream executes a live multi-turn agentic test with large context
// against opencode.ai (skipped unless OPENCODE_LANE_PROBE=1).
func TestLiveAgenticMultiTurnUpstream(t *testing.T) {
	if os.Getenv("OPENCODE_LANE_PROBE") != "1" {
		t.Skip("skipping live test; set OPENCODE_LANE_PROBE=1 to run against live upstream")
	}

	cfg := config.Default()
	up := NewUpstream(cfg, nil)
	fp := Fingerprint{
		SessionID: TranslateSessionID("cline-agent-session", "cline"),
		Project:   UpstreamProject,
	}

	largeContext := generateLargeText("You are an agentic AI pair programmer assisting with a Go microservice.\nCodebase overview:\n", 80)

	// Turn 1: User sends prompt with large context
	turn1Body := map[string]any{
		"model":  "big-pickle",
		"stream": true,
		"messages": []any{
			map[string]any{"role": "system", "content": largeContext},
			map[string]any{"role": "user", "content": "Briefly state your identity in 1 short sentence."},
		},
		"tools": []any{
			map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        "execute_command",
					"description": "Run shell command",
					"parameters": map[string]any{
						"type":       "object",
						"properties": map[string]any{"command": map[string]any{"type": "string"}},
						"required":   []string{"command"},
					},
				},
			},
		},
	}

	prep1, err := Prepare(cfg, turn1Body)
	if err != nil {
		t.Fatalf("Prepare turn 1 failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel()

	resp1, err := up.Send(ctx, cfg, prep1, fp)
	if err != nil {
		t.Fatalf("Live Turn 1 failed: %v", err)
	}

	if resp1.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp1.Body)
		t.Fatalf("Live Turn 1 HTTP status = %d: %s", resp1.StatusCode, string(body))
	}

	var turn1AssistantText string
	_ = ScanSSE(resp1.Body, func(ev SSEEvent) (bool, error) {
		var chunk map[string]any
		if err := json.Unmarshal(ev.Data, &chunk); err == nil {
			if choices, ok := chunk["choices"].([]any); ok && len(choices) > 0 {
				choice := choices[0].(map[string]any)
				if delta, ok := choice["delta"].(map[string]any); ok {
					if c, ok := delta["content"].(string); ok {
						turn1AssistantText += c
					}
				}
			}
		}
		return false, nil
	})

	_ = resp1.Body.Close()
	t.Logf("Live Turn 1 Response: %s", strings.TrimSpace(turn1AssistantText))

	// Turn 2: Follow-up using same session and conversation history
	turn2Body := map[string]any{
		"model":  "big-pickle",
		"stream": true,
		"messages": []any{
			map[string]any{"role": "system", "content": largeContext},
			map[string]any{"role": "user", "content": "Briefly state your identity in 1 short sentence."},
			map[string]any{"role": "assistant", "content": turn1AssistantText},
			map[string]any{"role": "user", "content": "Confirm in 3 words that you received the context."},
		},
	}

	prep2, err := Prepare(cfg, turn2Body)
	if err != nil {
		t.Fatalf("Prepare turn 2 failed: %v", err)
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 75*time.Second)
	defer cancel2()

	resp2, err := up.Send(ctx2, cfg, prep2, fp)
	if err != nil {
		t.Fatalf("Live Turn 2 failed: %v", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp2.Body)
		t.Fatalf("Live Turn 2 HTTP status = %d: %s", resp2.StatusCode, string(body))
	}

	var turn2AssistantText string
	_ = ScanSSE(resp2.Body, func(ev SSEEvent) (bool, error) {
		var chunk map[string]any
		if err := json.Unmarshal(ev.Data, &chunk); err == nil {
			if choices, ok := chunk["choices"].([]any); ok && len(choices) > 0 {
				choice := choices[0].(map[string]any)
				if delta, ok := choice["delta"].(map[string]any); ok {
					if c, ok := delta["content"].(string); ok {
						turn2AssistantText += c
					}
				}
			}
		}
		return false, nil
	})

	t.Logf("Live Turn 2 Response: %s", strings.TrimSpace(turn2AssistantText))
}

func sendClientRequest(handler http.Handler, payload map[string]any) (*http.Response, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer sk-opencode-free")
	req.Header.Set("x-client-name", "cline")

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec.Result(), nil
}