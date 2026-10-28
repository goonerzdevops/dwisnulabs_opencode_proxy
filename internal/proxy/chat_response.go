package proxy

import (
	"bufio"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// hopByHopHeaders must not be forwarded to the client.
var hopByHopHeaders = map[string]bool{
	"connection":          true,
	"keep-alive":          true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"te":                  true,
	"trailer":             true,
	"transfer-encoding":   true,
	"upgrade":             true,
	"content-length":      true,
}

// WriteChatPassthrough forwards a chat-lane upstream response verbatim, restoring
// caller tool names when the fingerprint had to rename a colliding tool.
func WriteChatPassthrough(w http.ResponseWriter, resp *http.Response, renames RenameMap, streaming bool) error {
	defer resp.Body.Close()

	if len(renames) == 0 {
		copyHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, err := io.Copy(w, resp.Body)
		return err
	}

	if streaming {
		copyHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		return copySSERestoringNames(w, resp.Body, renames)
	}

	blob, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	var payload map[string]any
	if err := json.Unmarshal(blob, &payload); err != nil {
		copyHeaders(w.Header(), resp.Header)
		w.WriteHeader(resp.StatusCode)
		_, writeErr := w.Write(blob)
		return writeErr
	}
	restoreNames(payload, renames)
	copyHeaders(w.Header(), resp.Header)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(resp.StatusCode)
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	return encoder.Encode(payload)
}

func copyHeaders(dst, src http.Header) {
	for key, values := range src {
		if hopByHopHeaders[strings.ToLower(key)] {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

// StreamChatSSEWithTelemetry streams SSE lines to w, restoring tool names if needed,
// and extracts actual model and token usage metrics while passing through.
func StreamChatSSEWithTelemetry(w http.ResponseWriter, src io.Reader, renames RenameMap) (actualModel string, promptTokens, cachedTokens, outputTokens int, err error) {
	flusher, _ := w.(http.Flusher)
	reader := bufio.NewReaderSize(src, 64*1024)
	outWords := 0

	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			replaced := line
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "data:") {
				payloadText := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
				if payloadText != "" && payloadText != "[DONE]" {
					var payload map[string]any
					if jsonErr := json.Unmarshal([]byte(payloadText), &payload); jsonErr == nil {
						if m := stringField(payload, "model"); m != "" && actualModel == "" {
							actualModel = m
						}
						if usage, ok := payload["usage"].(map[string]any); ok && len(usage) > 0 {
							promptTokens = int(numberField(usage, "prompt_tokens"))
							outputTokens = int(numberField(usage, "completion_tokens"))
							if promptDetails, ok := usage["prompt_tokens_details"].(map[string]any); ok {
								cachedTokens = int(numberField(promptDetails, "cached_tokens"))
							}
						}
						if choices, ok := payload["choices"].([]any); ok && len(choices) > 0 {
							if ch, ok := choices[0].(map[string]any); ok {
								if delta, ok := ch["delta"].(map[string]any); ok {
									if c := stringField(delta, "content"); c != "" {
										outWords += len(strings.Fields(c))
									}
									if r := stringField(delta, "reasoning_content"); r != "" {
										outWords += len(strings.Fields(r))
									}
								}
							}
						}
						if len(renames) > 0 {
							restoreNames(payload, renames)
							blob, mErr := json.Marshal(payload)
							if mErr == nil {
								suffix := ""
								if strings.HasSuffix(line, "\n") {
									suffix = "\n"
								}
								replaced = "data: " + string(blob) + suffix
							}
						}
					}
				}
			}

			if _, writeErr := io.WriteString(w, replaced); writeErr != nil {
				return actualModel, promptTokens, cachedTokens, outputTokens, writeErr
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return actualModel, promptTokens, cachedTokens, outputTokens, readErr
		}
	}

	if outputTokens == 0 && outWords > 0 {
		outputTokens = outWords
	}
	return actualModel, promptTokens, cachedTokens, outputTokens, nil
}

// copySSERestoringNames streams SSE lines, rewriting only the payloads that carry
// a renamed tool name so all other bytes reach the client untouched.
func copySSERestoringNames(w http.ResponseWriter, src io.Reader, renames RenameMap) error {
	_, _, _, _, err := StreamChatSSEWithTelemetry(w, src, renames)
	return err
}

// restoreSSELine rewrites one `data: {...}` line, leaving comments, event names
// and terminators as they are.
func restoreSSELine(line string, renames RenameMap) string {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "data:") {
		return line
	}
	payloadText := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if payloadText == "" || payloadText == "[DONE]" {
		return line
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadText), &payload); err != nil {
		return line
	}
	restoreNames(payload, renames)
	blob, err := json.Marshal(payload)
	if err != nil {
		return line
	}
	suffix := ""
	if strings.HasSuffix(line, "\n") {
		suffix = "\n"
	}
	return "data: " + string(blob) + suffix
}

// restoreNames reverses the fingerprint rename map in every OpenAI/Responses
// response shape that can carry a tool name.
func restoreNames(payload map[string]any, renames RenameMap) {
	if len(renames) == 0 || payload == nil {
		return
	}

	if choices, ok := payload["choices"].([]any); ok {
		for _, raw := range choices {
			choice, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			for _, holder := range []string{"delta", "message"} {
				container, ok := choice[holder].(map[string]any)
				if !ok {
					continue
				}
				calls, ok := container["tool_calls"].([]any)
				if !ok {
					continue
				}
				for _, rawCall := range calls {
					call, ok := rawCall.(map[string]any)
					if !ok {
						continue
					}
					fn, ok := call["function"].(map[string]any)
					if !ok {
						continue
					}
					if name, _ := fn["name"].(string); name != "" {
						fn["name"] = renames.Restore(name)
					}
				}
			}
		}
	}

	if item, ok := payload["item"].(map[string]any); ok {
		restoreItemName(item, renames)
	}
	if output, ok := payload["output"].([]any); ok {
		for _, raw := range output {
			if item, ok := raw.(map[string]any); ok {
				restoreItemName(item, renames)
			}
		}
	}
	if response, ok := payload["response"].(map[string]any); ok {
		if output, ok := response["output"].([]any); ok {
			for _, raw := range output {
				if item, ok := raw.(map[string]any); ok {
					restoreItemName(item, renames)
				}
			}
		}
	}
}

func restoreItemName(item map[string]any, renames RenameMap) {
	if kind, _ := item["type"].(string); kind != "function_call" {
		return
	}
	if name, _ := item["name"].(string); name != "" {
		item["name"] = renames.Restore(name)
	}
}
