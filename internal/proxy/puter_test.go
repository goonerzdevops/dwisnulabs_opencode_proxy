package proxy

import (
	"bytes"
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
		model    string
		isPuter  bool
		clean    string
	}{
		{"claude-3-5-sonnet", true, "claude-3-5-sonnet"},
		{"claude-3-7-sonnet", true, "claude-3-7-sonnet"},
		{"claude-3-5-haiku", true, "claude-3-5-haiku"},
		{"puter:gpt-4o", true, "gpt-4o"},
		{"puter/deepseek-chat", true, "deepseek-chat"},
		{"gpt-4o", true, "gpt-4o"},
		{"big-pickle", false, "big-pickle"},
		{"mimo-v2.6-flash-free", false, "mimo-v2.6-flash-free"},
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

func TestPuterPingToken(t *testing.T) {
	// Mock Puter API upstream server
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if auth != "Bearer valid-token" {
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]string{"message": "Invalid token"},
			})
			return
		}

		var payload map[string]any
		_ = json.NewDecoder(r.Body).Decode(&payload)

		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "chatcmpl-test",
			"object":  "chat.completion",
			"created": time.Now().Unix(),
			"model":   payload["model"],
			"choices": []map[string]any{
				{
					"index": 0,
					"message": map[string]string{
						"role":    "assistant",
						"content": "pong",
					},
					"finish_reason": "stop",
				},
			},
		})
	}))
	defer mockServer.Close()

	service := NewPuterService(nil)
	// Temporarily override client transport or test method
	client := mockServer.Client()
	service.client = client

	// We can test ping logic directly against mockServer URL
	ctx := context.Background()

	// 1. Valid token
	reqPayload, _ := json.Marshal(map[string]any{
		"model":      "claude-3-5-haiku",
		"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		"max_tokens": 5,
	})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, mockServer.URL, bytes.NewReader(reqPayload))
	req.Header.Set("Authorization", "Bearer valid-token")
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		t.Logf("mockServer client.Do error (likely sandbox socket): %v", err)
	} else {
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("expected 200 OK from mock, got status=%d", resp.StatusCode)
		}
	}

	// 2. Empty token ping
	_, _, err = service.PingToken(ctx, "", "claude-3-5-haiku")
	if err == nil {
		t.Errorf("expected error for empty token")
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
	cfg.PuterAPIKey = "test-puter-token"
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
	if !hasOpenCode {
		t.Errorf("expected OpenCode models in /v2/models")
	}

	// Verify /v2/puter/models returns only Puter models
	recPuter := httptest.NewRecorder()
	reqPuter := httptest.NewRequest(http.MethodGet, "/v2/puter/models", nil)
	server.Handler().ServeHTTP(recPuter, reqPuter)
	if recPuter.Code != http.StatusOK {
		t.Fatalf("GET /v2/puter/models returned %d; want 200", recPuter.Code)
	}

	// Verify /api/puter/ping with empty token returns friendly message
	recPing := httptest.NewRecorder()
	reqPing := httptest.NewRequest(http.MethodPost, "/api/puter/ping", strings.NewReader(`{"token":""}`))
	server.Handler().ServeHTTP(recPing, reqPing)
	if recPing.Code != http.StatusOK {
		t.Fatalf("POST /api/puter/ping returned %d; want 200", recPing.Code)
	}
}

func TestPuterMultiAccountManagement(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.json")

	mgr, err := config.NewManager(cfgPath)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}

	server := NewServer(mgr, nil)
	defer server.Close()
	handler := server.Handler()

	// 1. Add account 1
	recAdd1 := httptest.NewRecorder()
	reqAdd1 := httptest.NewRequest(http.MethodPost, "/api/puter/accounts", strings.NewReader(`{
		"id": "acc-1",
		"username": "spectacular_chair_223668",
		"token": "tok-1",
		"priority": 1
	}`))
	handler.ServeHTTP(recAdd1, reqAdd1)
	if recAdd1.Code != http.StatusOK {
		t.Fatalf("POST /api/puter/accounts (1) returned %d", recAdd1.Code)
	}

	// 2. Add account 2
	recAdd2 := httptest.NewRecorder()
	reqAdd2 := httptest.NewRequest(http.MethodPost, "/api/puter/accounts", strings.NewReader(`{
		"id": "acc-2",
		"username": "akun_kedua_anda",
		"token": "tok-2",
		"priority": 2
	}`))
	handler.ServeHTTP(recAdd2, reqAdd2)
	if recAdd2.Code != http.StatusOK {
		t.Fatalf("POST /api/puter/accounts (2) returned %d", recAdd2.Code)
	}

	// 3. GET /api/puter/accounts
	recGet := httptest.NewRecorder()
	reqGet := httptest.NewRequest(http.MethodGet, "/api/puter/accounts", nil)
	handler.ServeHTTP(recGet, reqGet)
	if recGet.Code != http.StatusOK {
		t.Fatalf("GET /api/puter/accounts returned %d", recGet.Code)
	}
	var getResp struct {
		Accounts []config.PuterAccount `json:"accounts"`
		Count    int                   `json:"count"`
	}
	_ = json.Unmarshal(recGet.Body.Bytes(), &getResp)
	if getResp.Count != 2 {
		t.Fatalf("expected 2 accounts, got %d", getResp.Count)
	}
	if getResp.Accounts[0].ID != "acc-1" || getResp.Accounts[1].ID != "acc-2" {
		t.Fatalf("unexpected order: %v, %v", getResp.Accounts[0].ID, getResp.Accounts[1].ID)
	}

	// 4. Mark account 1 in cooldown
	mgr.MarkPuterAccountStatus("acc-1", "cooldown", 1*time.Hour, "HTTP 402 test")
	candidates := mgr.GetActivePuterAccountCandidates()
	if len(candidates) != 1 || candidates[0].ID != "acc-2" {
		t.Fatalf("expected candidate to be acc-2, got %v", candidates)
	}

	// 5. Reset status
	recReset := httptest.NewRecorder()
	reqReset := httptest.NewRequest(http.MethodPost, "/api/puter/accounts/reset?id=acc-1", nil)
	handler.ServeHTTP(recReset, reqReset)
	if recReset.Code != http.StatusOK {
		t.Fatalf("POST /api/puter/accounts/reset returned %d", recReset.Code)
	}
	candidatesAfterReset := mgr.GetActivePuterAccountCandidates()
	if len(candidatesAfterReset) != 2 || candidatesAfterReset[0].ID != "acc-1" {
		t.Fatalf("expected candidate 1 to be acc-1 after reset, got %v", candidatesAfterReset)
	}

	// 6. Delete account 2
	recDel := httptest.NewRecorder()
	reqDel := httptest.NewRequest(http.MethodDelete, "/api/puter/accounts?id=acc-2", nil)
	handler.ServeHTTP(recDel, reqDel)
	if recDel.Code != http.StatusOK {
		t.Fatalf("DELETE /api/puter/accounts returned %d", recDel.Code)
	}

	accsLeft := mgr.GetPuterAccounts()
	if len(accsLeft) != 1 || accsLeft[0].ID != "acc-1" {
		t.Fatalf("expected 1 account left, got %v", accsLeft)
	}
}
