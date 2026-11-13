package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

func decodeJSONMap(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	return m, nil
}

func firstChoice(m map[string]any) map[string]any {
	choices, _ := m["choices"].([]any)
	if len(choices) == 0 {
		return nil
	}
	c, _ := choices[0].(map[string]any)
	return c
}

// rewriteChatCompletionToolCalls converts textual tool calls inside a non-streaming
// chat completion into structured tool_calls. The blob is returned unchanged when
// there is nothing to convert.
func rewriteChatCompletionToolCalls(blob []byte, schemas toolSchemas) []byte {
	m, err := decodeJSONMap(blob)
	if err != nil {
		return blob
	}
	choice := firstChoice(m)
	if choice == nil {
		return blob
	}
	msg, _ := choice["message"].(map[string]any)
	content, _ := msg["content"].(string)
	if msg == nil || content == "" || !HasDSML(content) {
		return blob
	}
	if existing, ok := msg["tool_calls"].([]any); ok && len(existing) > 0 {
		return blob
	}
	clean, calls := ParseToolCalls(content, schemas)
	if len(calls) == 0 {
		return blob
	}
	msg["content"] = clean
	msg["tool_calls"] = calls
	choice["finish_reason"] = "tool_calls"
	out, err := json.Marshal(m)
	if err != nil {
		return blob
	}
	return out
}

// streamPuterSSEWithToolParsing copies an OpenAI SSE stream to the client while converting
// textual (DSML / XML) tool calls in delta.content into structured tool_calls deltas.
func streamPuterSSEWithToolParsing(src io.Reader, w http.ResponseWriter, schemas toolSchemas) {
	flusher, isFlusher := w.(http.Flusher)
	flush := func() {
		if isFlusher {
			flusher.Flush()
		}
	}
	writeData := func(obj map[string]any) {
		raw, err := json.Marshal(obj)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", raw)
		flush()
	}

	filter := newToolMarkupFilter(schemas)
	finished := false
	sawChunk := false
	var meta map[string]any

	newChunk := func(delta map[string]any, finish any) map[string]any {
		obj := map[string]any{
			"object": "chat.completion.chunk",
			"choices": []any{
				map[string]any{"index": 0, "delta": delta, "finish_reason": finish},
			},
		}
		for _, k := range []string{"id", "created", "model"} {
			if v, ok := meta[k]; ok {
				obj[k] = v
			}
		}
		return obj
	}

	// finish flushes buffered text / tool calls and terminates the choice.
	finish := func(reason any) {
		if finished {
			return
		}
		finished = true
		leftover, calls := filter.Finish()
		if leftover != "" {
			writeData(newChunk(map[string]any{"content": leftover}, nil))
		}
		if len(calls) > 0 {
			writeData(newChunk(map[string]any{"tool_calls": calls}, nil))
			reason = "tool_calls"
		}
		if reason == nil {
			reason = "stop"
		}
		writeData(newChunk(map[string]any{}, reason))
	}

	reader := bufio.NewReaderSize(src, 64*1024)
	doneSent := false
	for {
		line, readErr := reader.ReadString('\n')
		line = strings.TrimRight(line, "\r\n")

		switch {
		case line == "":
			// blank separators are re-created per data frame
		case strings.HasPrefix(line, "data:"):
			payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if payload == "[DONE]" {
				if sawChunk {
					finish(nil)
				}
				fmt.Fprint(w, "data: [DONE]\n\n")
				flush()
				doneSent = true
				break
			}
			obj, err := decodeJSONMap([]byte(payload))
			choice := firstChoice(obj)
			if err != nil || choice == nil {
				fmt.Fprintf(w, "%s\n\n", line)
				flush()
				break
			}
			sawChunk = true
			meta = obj
			if finished {
				// upstream sent extra frames after we terminated (e.g. usage); forward as-is
				fmt.Fprintf(w, "%s\n\n", line)
				flush()
				break
			}

			delta, _ := choice["delta"].(map[string]any)
			if delta == nil {
				delta = map[string]any{}
				choice["delta"] = delta
			}
			if text, ok := delta["content"].(string); ok && text != "" {
				if out := filter.Push(text); out != "" {
					delta["content"] = out
				} else {
					delete(delta, "content")
				}
			}

			reason := choice["finish_reason"]
			if reason != nil {
				choice["finish_reason"] = nil
			}
			_, hasUsage := obj["usage"]
			if len(delta) > 0 || hasUsage {
				writeData(obj)
			}
			if reason != nil {
				finish(reason)
			}
		default:
			fmt.Fprintf(w, "%s\n", line)
			flush()
		}

		if readErr != nil {
			break
		}
	}

	if !doneSent {
		if sawChunk {
			finish(nil)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		flush()
	}
}
