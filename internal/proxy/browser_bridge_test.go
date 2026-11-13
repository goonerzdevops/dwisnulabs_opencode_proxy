package proxy

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestBrowserBridgeLifecycle(t *testing.T) {
	bridge := NewBrowserBridge(nil)

	if bridge.IsConnected() {
		t.Errorf("expected bridge not connected initially")
	}

	ready, _ := bridge.IsReady()
	if ready {
		t.Errorf("expected bridge not ready initially")
	}

	// Status endpoint test
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/browser-relay/status", nil)
	bridge.HandleStatus(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("HandleStatus returned %d; want 200", rec.Code)
	}

	var status map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&status); err != nil {
		t.Fatalf("failed to decode status: %v", err)
	}

	if status["connected"] != false {
		t.Errorf("expected connected=false, got %v", status["connected"])
	}

	// Execute without worker should fail gracefully
	recExec := httptest.NewRecorder()
	err := bridge.Execute(context.Background(), "claude-opus-5-5", map[string]any{
		"messages": []map[string]string{{"role": "user", "content": "hello"}},
	}, recExec)

	if err == nil {
		t.Errorf("expected error when executing without worker")
	}
}

func TestBrowserBridgeReadyAndChunks(t *testing.T) {
	bridge := NewBrowserBridge(nil)

	// Test HandleReady endpoint
	recReady := httptest.NewRecorder()
	reqReady := httptest.NewRequest(http.MethodPost, "/api/browser-relay/ready", strings.NewReader(`{"signed_in":true,"username":"alice"}`))
	bridge.HandleReady(recReady, reqReady)

	if recReady.Code != http.StatusOK {
		t.Fatalf("HandleReady returned %d; want 200", recReady.Code)
	}

	// Fake an active event channel
	ch := make(chan []byte, 10)
	bridge.mu.Lock()
	bridge.eventCh = ch
	bridge.mu.Unlock()

	ready, user := bridge.IsReady()
	if !ready || user != "alice" {
		t.Errorf("expected ready=true, user=alice; got ready=%v, user=%s", ready, user)
	}

	// Setup a job channel
	jobCh := make(chan BridgeMessage, 10)
	bridge.jobsMu.Lock()
	bridge.jobs["job-test"] = jobCh
	bridge.jobsMu.Unlock()

	// Test HandleChunk
	recChunk := httptest.NewRecorder()
	reqChunk := httptest.NewRequest(http.MethodPost, "/api/browser-relay/chunk", strings.NewReader(`{"id":"job-test","text":"Hello world"}`))
	bridge.HandleChunk(recChunk, reqChunk)

	select {
	case msg := <-jobCh:
		if msg.Text != "Hello world" || msg.Type != "chunk" {
			t.Errorf("unexpected message: %+v", msg)
		}
	case <-time.After(500 * time.Millisecond):
		t.Errorf("timeout waiting for chunk on job channel")
	}

	// Test HandleDone
	recDone := httptest.NewRecorder()
	reqDone := httptest.NewRequest(http.MethodPost, "/api/browser-relay/done", strings.NewReader(`{"id":"job-test"}`))
	bridge.HandleDone(recDone, reqDone)

	select {
	case msg := <-jobCh:
		if msg.Type != "done" {
			t.Errorf("unexpected message: %+v", msg)
		}
	case <-time.After(500 * time.Millisecond):
		t.Errorf("timeout waiting for done on job channel")
	}
}

func TestBrowserBridgeExecuteStreamingFinishReason(t *testing.T) {
	bridge := NewBrowserBridge(nil)

	// Set ready
	bridge.mu.Lock()
	bridge.eventCh = make(chan []byte, 10)
	bridge.signedIn = true
	bridge.username = "testuser"
	bridge.mu.Unlock()

	rec := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	errCh := make(chan error, 1)
	go func() {
		errCh <- bridge.Execute(ctx, "deepseek-v4.1-flash-1017", map[string]any{
			"stream":   true,
			"messages": []map[string]string{{"role": "user", "content": "hi"}},
		}, rec)
	}()

	// Read event dispatched to browser worker
	var eventBytes []byte
	select {
	case eventBytes = <-bridge.eventCh:
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for task on eventCh")
	}

	// Parse job ID from event
	// Format is "event: chat\ndata: {json}\n\n"
	lines := strings.Split(string(eventBytes), "\n")
	var dataLine string
	for _, l := range lines {
		if strings.HasPrefix(l, "data: ") {
			dataLine = strings.TrimPrefix(l, "data: ")
			break
		}
	}
	if dataLine == "" {
		t.Fatalf("data line not found in event: %s", string(eventBytes))
	}

	var task map[string]any
	if err := json.Unmarshal([]byte(dataLine), &task); err != nil {
		t.Fatalf("failed to unmarshal task: %v", err)
	}
	jobID, _ := task["id"].(string)
	if jobID == "" {
		t.Fatalf("job id empty in task: %+v", task)
	}

	// Send chunk
	recChunk := httptest.NewRecorder()
	reqChunk := httptest.NewRequest(http.MethodPost, "/api/browser-relay/chunk", strings.NewReader(`{"id":"`+jobID+`","text":"Halo dunia"}`))
	bridge.HandleChunk(recChunk, reqChunk)

	// Send done
	recDone := httptest.NewRecorder()
	reqDone := httptest.NewRequest(http.MethodPost, "/api/browser-relay/done", strings.NewReader(`{"id":"`+jobID+`"}`))
	bridge.HandleDone(recDone, reqDone)

	// Wait for Execute to finish
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Execute returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for Execute to return")
	}

	respBody := rec.Body.String()
	// Verify that response has finish_reason: "stop"
	if !strings.Contains(respBody, `"finish_reason":"stop"`) {
		t.Errorf("expected stream to contain finish_reason:stop, got:\n%s", respBody)
	}
	// Verify data: [DONE] is at the end
	if !strings.Contains(respBody, "data: [DONE]\n\n") {
		t.Errorf("expected stream to end with [DONE], got:\n%s", respBody)
	}
	// Verify first chunk has role: assistant
	if !strings.Contains(respBody, `"role":"assistant"`) {
		t.Errorf("expected stream to contain role:assistant, got:\n%s", respBody)
	}
}

func TestBrowserBridgeExecuteStreamingDSML(t *testing.T) {
	bridge := NewBrowserBridge(nil)
	ch := make(chan []byte, 10)
	bridge.mu.Lock()
	bridge.eventCh = ch
	bridge.signedIn = true
	bridge.username = "alice"
	bridge.mu.Unlock()

	rec := httptest.NewRecorder()
	errCh := make(chan error, 1)

	go func() {
		err := bridge.Execute(context.Background(), "deepseek-v4.1-flash", map[string]any{
			"messages": []map[string]string{{"role": "user", "content": "read file"}},
			"stream":   true,
		}, rec)
		errCh <- err
	}()

	var eventBytes []byte
	select {
	case eventBytes = <-ch:
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for chat event on bridge channel")
	}

	lines := strings.Split(string(eventBytes), "\n")
	var dataLine string
	for _, l := range lines {
		if strings.HasPrefix(l, "data: ") {
			dataLine = strings.TrimPrefix(l, "data: ")
			break
		}
	}
	var task map[string]any
	json.Unmarshal([]byte(dataLine), &task)
	jobID, _ := task["id"].(string)

	// Send chunk 1: text
	recChunk1 := httptest.NewRecorder()
	reqChunk1 := httptest.NewRequest(http.MethodPost, "/api/browser-relay/chunk", strings.NewReader(`{"id":"`+jobID+`","text":"Saya akan periksa.\n\n"}`))
	bridge.HandleChunk(recChunk1, reqChunk1)

	// Send chunk 2: DSML
	recChunk2 := httptest.NewRecorder()
	dsmlPayload := `< | | DSML | | calls> < | | DSML | | invoke name="read"> < | | DSML | | parameter name="path" string="true">C:\file.md</ | | DSML | | parameter> </ | | DSML | | invoke> </ | | DSML | | calls>`
	dsmlJSON, _ := json.Marshal(map[string]string{"id": jobID, "text": dsmlPayload})
	reqChunk2 := httptest.NewRequest(http.MethodPost, "/api/browser-relay/chunk", strings.NewReader(string(dsmlJSON)))
	bridge.HandleChunk(recChunk2, reqChunk2)

	// Send done
	recDone := httptest.NewRecorder()
	reqDone := httptest.NewRequest(http.MethodPost, "/api/browser-relay/done", strings.NewReader(`{"id":"`+jobID+`"}`))
	bridge.HandleDone(recDone, reqDone)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Execute returned error: %v", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatalf("timed out waiting for Execute to return")
	}

	respBody := rec.Body.String()

	// Verify DSML was NOT leaked as plain text
	if strings.Contains(respBody, "< | | DSML") {
		t.Errorf("raw DSML leaked in response: %s", respBody)
	}

	// Verify tool_calls is emitted
	if !strings.Contains(respBody, `"tool_calls"`) {
		t.Errorf("expected tool_calls in stream, got:\n%s", respBody)
	}

	// Verify func name 'read'
	if !strings.Contains(respBody, `"name":"read"`) {
		t.Errorf("expected func name 'read' in stream, got:\n%s", respBody)
	}

	// Verify index:0 is explicitly present (required by OpenAI protocol for tool_calls)
	if !strings.Contains(respBody, `"index":0`) {
		t.Errorf("expected 'index':0 in tool_calls delta, got:\n%s", respBody)
	}

	// Verify finish_reason: "tool_calls"
	if !strings.Contains(respBody, `"finish_reason":"tool_calls"`) {
		t.Errorf("expected finish_reason:tool_calls, got:\n%s", respBody)
	}
}
