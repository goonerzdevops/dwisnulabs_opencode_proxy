package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"opencode-proxy/internal/config"
)

func TestIsPuterModelAndClean(t *testing.T) {
	cases := []struct {
		model   string
		isPuter bool
		clean   string
	}{
		{"claude-3-5-sonnet", true, "claude-3-5-sonnet"},
		{"claude-3-7-sonnet", true, "claude-3-7-sonnet"},
		{"claude-3-5-haiku", true, "claude-3-5-haiku"},
		{"puter:gpt-4o", true, "gpt-4o"},
		{"puter/deepseek-chat", true, "deepseek-chat"},
		{"gpt-4o", true, "gpt-4o"},
		{"gpt-5.4", true, "gpt-5.4"},
		{"claude-opus-5-5", true, "claude-opus-5-5"},
		{"claude-sonnet-5-5", true, "claude-sonnet-5-5"},
		{"claude-haiku-5-5", true, "claude-haiku-5-5"},
		{"claude-haiku-4.5", true, "claude-haiku-4-5-20251001"},
		{"deepseek-v4-pro", true, "deepseek-v4-pro"},
		{"deepseek-v4.1-flash-1017", true, "infron:deepseek/deepseek-v4.1-flash-1017"},
		{"infron:deepseek/deepseek-v4.1-flash-1017", true, "infron:deepseek/deepseek-v4.1-flash-1017"},
		{"puter:infron:deepseek/deepseek-v4.1-flash-1017", true, "infron:deepseek/deepseek-v4.1-flash-1017"},
		{"gemini-3.8-flash", true, "gemini-3.8-flash"},
		{"qwen3.5-flash", true, "qwen3.5-flash"},
		{"glm-5.3-prime", true, "glm-5.3-prime"},
		{"glm-5.3-flash-free", false, "glm-5.3-flash-free"},
		{"qwen3.5-coder-free", false, "qwen3.5-coder-free"},
		{"deepseek-v4.1-flash-free", false, "deepseek-v4.1-flash-free"},
		{"big-pickle", false, "big-pickle"},
		{"mimo-v2.6-flash-free", false, "mimo-v2.6-flash-free"},
		{"muse-spark-1.3-contributor-free", false, "muse-spark-1.3-contributor-free"},
		{"union-alpha", false, "union-alpha"},
	}

	for _, c := range cases {
		if got := IsPuterModel(c.model); got != c.isPuter {
			t.Errorf("IsPuterModel(%q) = %v; want %v", c.model, got, c.isPuter)
		}
		if got := CleanPuterModel(c.model); got != c.clean {
			t.Errorf("CleanPuterModel(%q) = %q; want %q", c.model, got, c.clean)
		}
	}
}

func TestPuterChatForwardingAndRouting(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.json")
	mgr, err := config.NewManager(cfgPath)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := mgr.Get()
	cfg.PuterEnabled = true
	if err := mgr.Update(cfg); err != nil {
		t.Fatalf("mgr.Update failed: %v", err)
	}

	server := NewServer(mgr, nil)
	defer server.Close()

	// Verify /v2/models returns both OpenCode and Puter models
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v2/models", nil)
	server.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /v2/models returned %d; want 200", rec.Code)
	}

	var modelsResp struct {
		Data []ModelEntry `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &modelsResp); err != nil {
		t.Fatalf("unmarshal /v2/models resp: %v", err)
	}

	hasPuter := false
	hasOpenCode := false
	for _, m := range modelsResp.Data {
		if m.OwnedBy == "puter" {
			hasPuter = true
		}
		if m.OwnedBy == "opencode" {
			hasOpenCode = true
		}
	}
	if !hasPuter {
		t.Errorf("expected Puter models in /v2/models")
	}
	if hasOpenCode {
		t.Errorf("expected OpenCode models to be excluded from /v2/models")
	}

	// Verify /v2/puter/models returns only Puter models
	recPuter := httptest.NewRecorder()
	reqPuter := httptest.NewRequest(http.MethodGet, "/v2/puter/models", nil)
	server.Handler().ServeHTTP(recPuter, reqPuter)
	if recPuter.Code != http.StatusOK {
		t.Fatalf("GET /v2/puter/models returned %d; want 200", recPuter.Code)
	}

	// Verify /v2/puter/chat/completions without connected browser bridge returns HTTP 503 (not 401 invalid api key)
	recChat := httptest.NewRecorder()
	reqChat := httptest.NewRequest(http.MethodPost, "/v2/puter/chat/completions", strings.NewReader(`{
		"model": "claude-haiku-5-5",
		"messages": [{"role": "user", "content": "hi"}]
	}`))
	reqChat.Header.Set("Content-Type", "application/json")
	server.Handler().ServeHTTP(recChat, reqChat)
	if recChat.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected HTTP 503 for disconnected browser bridge, got %d", recChat.Code)
	}
	var errResp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	_ = json.Unmarshal(recChat.Body.Bytes(), &errResp)
	if errResp.Error.Code != "browser_bridge_not_connected" {
		t.Errorf("expected code browser_bridge_not_connected, got %s", errResp.Error.Code)
	}
}

func TestPuterBrowserBridgeDelegation(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "config.json")
	mgr, err := config.NewManager(cfgPath)
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}

	cfg := mgr.Get()
	cfg.PuterEnabled = true
	if err := mgr.Update(cfg); err != nil {
		t.Fatalf("mgr.Update failed: %v", err)
	}

	server := NewServer(mgr, nil)
	defer server.Close()

	// Simulate browser tab connecting to SSE
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sseReq := httptest.NewRequest(http.MethodGet, "/api/browser-relay/events", nil).WithContext(ctx)
	sseRec := httptest.NewRecorder()
	go server.Handler().ServeHTTP(sseRec, sseReq)

	time.Sleep(50 * time.Millisecond)

	// Simulate browser reporting ready with logged-in user
	readyReq := httptest.NewRequest(http.MethodPost, "/api/browser-relay/ready", strings.NewReader(`{
		"signed_in": true,
		"username": "test_user_free"
	}`))
	readyReq.Header.Set("Content-Type", "application/json")
	readyRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(readyRec, readyReq)
	if readyRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from /api/browser-relay/ready, got %d", readyRec.Code)
	}

	ready, username := server.bridge.IsReady()
	if !ready || username != "test_user_free" {
		t.Fatalf("expected bridge ready with username test_user_free, got ready=%t, user=%s", ready, username)
	}
}
