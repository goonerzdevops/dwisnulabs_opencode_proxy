package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStandaloneProxyServerModels(t *testing.T) {
	client := NewRespanClient("http://127.0.0.1:9999", "test-key")
	server := &StandaloneProxyServer{
		client: client,
		port:   9999,
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var res struct {
		Object string           `json:"object"`
		Data   []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse models response: %v", err)
	}

	if len(res.Data) == 0 {
		t.Fatalf("expected models, got empty list")
	}

	foundCarbon := false
	for _, m := range res.Data {
		if m["id"] == DefaultModel {
			foundCarbon = true
			break
		}
	}
	if !foundCarbon {
		t.Errorf("model %q not found in models list", DefaultModel)
	}
}

func TestStandaloneProxyServerHealth(t *testing.T) {
	client := NewRespanClient("http://127.0.0.1:9999", "test-key")
	server := &StandaloneProxyServer{
		client: client,
		port:   9999,
	}

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()

	server.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var res map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to parse health response: %v", err)
	}

	if res["status"] != "running" {
		t.Errorf("expected status 'running', got %v", res["status"])
	}
}

