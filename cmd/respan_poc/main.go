package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	DefaultRespanBaseURL = "https://gateway.respan.ai"
	DefaultModel         = "google_gemini_ai/gemini-4-carbon:free"
)

// RespanMessage represents a standard chat message.
type RespanMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// RespanChatRequest represents OpenAI-compatible payload sent to Respan Gateway.
type RespanChatRequest struct {
	Model       string          `json:"model"`
	Messages    []RespanMessage `json:"messages"`
	Stream      bool            `json:"stream,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
}

// RespanClient is an independent HTTP client for Respan Gateway.
type RespanClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

// NewRespanClient creates a configured Respan client.
func NewRespanClient(baseURL, apiKey string) *RespanClient {
	if baseURL == "" {
		baseURL = DefaultRespanBaseURL
	}
	baseURL = strings.TrimRight(baseURL, "/")
	return &RespanClient{
		BaseURL: baseURL,
		APIKey:  apiKey,
		HTTPClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

// ExecuteChat sends chat completion request to Respan Gateway.
func (c *RespanClient) ExecuteChat(ctx context.Context, req RespanChatRequest, onChunk func(string)) error {
	url := c.BaseURL + "/v1/chat/completions"

	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("failed to marshal payload: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return fmt.Errorf("failed to create http request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "Respan-PoC-Client/1.0")
	if c.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.APIKey)
		httpReq.Header.Set("x-api-key", c.APIKey)
	}
	if req.Stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	} else {
		httpReq.Header.Set("Accept", "application/json")
	}

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return fmt.Errorf("respan gateway returned HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	if !req.Stream {
		respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return fmt.Errorf("failed to read response: %w", err)
		}
		var parsed struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(respBody, &parsed); err == nil && len(parsed.Choices) > 0 {
			if onChunk != nil {
				onChunk(parsed.Choices[0].Message.Content)
			}
		} else {
			if onChunk != nil {
				onChunk(string(respBody))
			}
		}
		return nil
	}

	// Handle SSE stream
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		payload := strings.TrimPrefix(line, "data: ")
		if strings.TrimSpace(payload) == "[DONE]" {
			break
		}

		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err == nil {
			if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
				if onChunk != nil {
					onChunk(chunk.Choices[0].Delta.Content)
				}
			}
		}
	}
	return scanner.Err()
}

// StandaloneProxyServer serves an OpenAI-compatible proxy forwarding to Respan Gateway.
type StandaloneProxyServer struct {
	client *RespanClient
	port   int
}

func (s *StandaloneProxyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Enable CORS for testing
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	switch {
	case r.URL.Path == "/v1/models" || r.URL.Path == "/models":
		s.handleModels(w, r)
	case r.URL.Path == "/v1/chat/completions" || r.URL.Path == "/chat/completions":
		s.handleChatCompletions(w, r)
	case r.URL.Path == "/" || r.URL.Path == "/health":
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"status":   "running",
			"proxy":    "Respan Gateway Independent PoC Proxy",
			"base_url": s.client.BaseURL,
			"has_key":  s.client.APIKey != "",
		})
	default:
		http.NotFound(w, r)
	}
}

func (s *StandaloneProxyServer) handleModels(w http.ResponseWriter, r *http.Request) {
	models := []map[string]any{
		{"id": DefaultModel, "object": "model", "owned_by": "respan-google"},
		{"id": "gemini-2.5-flash", "object": "model", "owned_by": "respan-google"},
		{"id": "gemini-2.5-pro", "object": "model", "owned_by": "respan-google"},
		{"id": "gpt-5.5", "object": "model", "owned_by": "respan-openai"},
		{"id": "claude-sonnet-4-5-20250929", "object": "model", "owned_by": "respan-anthropic"},
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	json.NewEncoder(w).Encode(map[string]any{
		"object": "list",
		"data":   models,
	})
}

func (s *StandaloneProxyServer) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, `{"error":"Method not allowed"}`, http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 5<<20))
	if err != nil {
		http.Error(w, `{"error":"Failed to read request body"}`, http.StatusBadRequest)
		return
	}

	var reqBody map[string]any
	if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
		http.Error(w, `{"error":"Invalid JSON payload"}`, http.StatusBadRequest)
		return
	}

	// Model normalization
	reqModel, _ := reqBody["model"].(string)
	if reqModel == "" {
		reqModel = DefaultModel
		reqBody["model"] = reqModel
	}

	apiKey := s.client.APIKey
	// Allow client Authorization header override if present
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		clientToken := strings.TrimPrefix(authHeader, "Bearer ")
		if clientToken != "" && clientToken != "sk-opencode-free" && clientToken != "sk-dummy" {
			apiKey = clientToken
		}
	}

	isStream, _ := reqBody["stream"].(bool)
	upstreamPayload, _ := json.Marshal(reqBody)

	upReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, s.client.BaseURL+"/v1/chat/completions", bytes.NewReader(upstreamPayload))
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"Failed to prepare upstream request: %s"}`, err.Error()), http.StatusInternalServerError)
		return
	}

	upReq.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		upReq.Header.Set("Authorization", "Bearer "+apiKey)
		upReq.Header.Set("x-api-key", apiKey)
	}
	if isStream {
		upReq.Header.Set("Accept", "text/event-stream")
	} else {
		upReq.Header.Set("Accept", "application/json")
	}

	resp, err := s.client.HTTPClient.Do(upReq)
	if err != nil {
		http.Error(w, fmt.Sprintf(`{"error":"Upstream Respan Gateway unreachable: %s"}`, err.Error()), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Forward response headers
	for k, vv := range resp.Header {
		for _, v := range vv {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	if isStream {
		flusher, ok := w.(http.Flusher)
		buf := make([]byte, 4096)
		for {
			n, readErr := resp.Body.Read(buf)
			if n > 0 {
				w.Write(buf[:n])
				if ok {
					flusher.Flush()
				}
			}
			if readErr != nil {
				break
			}
		}
	} else {
		io.Copy(w, resp.Body)
	}
}

func main() {
	mode := flag.String("mode", "cli", "Running mode: 'cli' (direct test prompt) or 'proxy' (standalone local gateway server)")
	port := flag.Int("port", 20145, "Port for standalone proxy server")
	apiKey := flag.String("key", "", "Respan API Key (falls back to RESPAN_API_KEY env var)")
	model := flag.String("model", DefaultModel, "Model ID to test")
	prompt := flag.String("prompt", "Halo! Jelaskan dalam 1 kalimat identitasmu dan model apa yang sedang aktif.", "Prompt to send in cli mode")
	stream := flag.Bool("stream", true, "Use SSE streaming output")
	flag.Parse()

	keyVal := *apiKey
	if keyVal == "" {
		keyVal = os.Getenv("RESPAN_API_KEY")
	}
	if keyVal == "" {
		for _, fname := range []string{"respan_token.txt", ".respan_token.txt"} {
			if content, err := os.ReadFile(fname); err == nil {
				keyVal = strings.TrimSpace(string(content))
				if keyVal != "" {
					break
				}
			}
		}
	}

	client := NewRespanClient(DefaultRespanBaseURL, keyVal)

	fmt.Println("=================================================================")
	fmt.Println(" Respan Gateway Independent PoC (Proof-of-Concept)")
	fmt.Println(" Reference: https://www.respan.ai/docs/agent-setup.md")
	fmt.Println("=================================================================")
	fmt.Printf("[CONFIG] Base URL : %s\n", client.BaseURL)
	fmt.Printf("[CONFIG] Model    : %s\n", *model)
	if keyVal != "" {
		masked := keyVal
		if len(masked) > 8 {
			masked = masked[:4] + "..." + masked[len(masked)-4:]
		}
		fmt.Printf("[CONFIG] API Key  : %s (Configured)\n", masked)
	} else {
		fmt.Printf("[CONFIG] API Key  : NOT SET (Required by upstream https://gateway.respan.ai)\n")
	}

	if *mode == "proxy" {
		addr := fmt.Sprintf("127.0.0.1:%d", *port)
		server := &StandaloneProxyServer{
			client: client,
			port:   *port,
		}
		httpServer := &http.Server{
			Addr:    addr,
			Handler: server,
		}

		fmt.Println("\n-----------------------------------------------------------------")
		fmt.Printf("[SERVER] Standalone Respan Proxy listening on: http://%s\n", addr)
		fmt.Printf("[SERVER] OpenAI Endpoint : http://%s/v1/chat/completions\n", addr)
		fmt.Printf("[SERVER] Models Endpoint : http://%s/v1/models\n", addr)
		fmt.Println("-----------------------------------------------------------------")
		fmt.Println("Example DeepSeek Harness / Cline setup:")
		fmt.Printf("  Base URL: http://%s\n", addr)
		fmt.Printf("  Model   : %s\n", *model)
		fmt.Printf("  API Key : %s\n", func() string {
			if keyVal != "" {
				return "(using configured RESPAN_API_KEY)"
			}
			return "sk-dummy (upstream will require valid RESPAN_API_KEY)"
		}())
		fmt.Println("Press Ctrl+C to stop.")

		stopChan := make(chan os.Signal, 1)
		signal.Notify(stopChan, os.Interrupt, syscall.SIGTERM)
		go func() {
			if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				log.Fatalf("[ERROR] Proxy server error: %v", err)
			}
		}()

		<-stopChan
		fmt.Println("\n[SERVER] Shutting down proxy server...")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = httpServer.Shutdown(ctx)
		fmt.Println("[SERVER] Proxy server stopped.")
		return
	}

	// CLI Direct Test Mode
	fmt.Println("\n[TEST] Sending direct request to Respan Gateway...")
	fmt.Printf("[PROMPT] \"%s\"\n\n", *prompt)
	fmt.Print("[OUTPUT] ")

	req := RespanChatRequest{
		Model: *model,
		Messages: []RespanMessage{
			{Role: "user", Content: *prompt},
		},
		Stream: *stream,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	err := client.ExecuteChat(ctx, req, func(chunk string) {
		fmt.Print(chunk)
	})

	fmt.Println()
	if err != nil {
		fmt.Printf("\n[RESULT] Request completed with status/error:\n  %v\n", err)
		if strings.Contains(err.Error(), "401") || strings.Contains(err.Error(), "API key is invalid") {
			fmt.Println("\n[ANALYSIS]")
			fmt.Println("  1. Upstream Respan Gateway reachable: YES (https://gateway.respan.ai responded)")
			fmt.Println("  2. Autentikasi: Respan Gateway memerlukan RESPAN_API_KEY aktif.")
			fmt.Println("  3. Cara mendapatkan key: Daftar di https://platform.respan.ai/signup lalu buat key di https://platform.respan.ai/platform/gateway/api-keys")
		}
	} else {
		fmt.Println("\n[RESULT] Request successfully completed!")
	}
}

