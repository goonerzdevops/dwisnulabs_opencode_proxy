package proxy

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func readSchemas() toolSchemas {
	return toolSchemas{
		"read": &ToolSpec{
			Properties: map[string]string{"file_path": "string", "offset": "integer", "limit": "integer"},
			Required:   map[string]bool{"file_path": true},
		},
	}
}

func decodeArgs(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatalf("bad args json %q: %v", raw, err)
	}
	return m
}

func TestParseToolCallsAnthropicXML(t *testing.T) {
	// Exact shape leaked by claude-haiku-5-5 in DeepSeek Harness.
	in := "Saya baca dulu file-nya untuk melihat bagian alur yang dimaksud.\n\n" +
		`<tool_call> <invoke name="read"> <parameter name="path">C:\D\Project\Microservices\TECH_STACK_BLUEPRINT_2026.md</parameter> </invoke> </tool_call>`
	clean, calls := ParseToolCalls(in, readSchemas())
	if clean != "Saya baca dulu file-nya untuk melihat bagian alur yang dimaksud." {
		t.Errorf("clean = %q", clean)
	}
	if len(calls) != 1 || calls[0].Function.Name != "read" {
		t.Fatalf("calls = %+v", calls)
	}
	args := decodeArgs(t, calls[0].Function.Arguments)
	if args["file_path"] != `C:\D\Project\Microservices\TECH_STACK_BLUEPRINT_2026.md` {
		t.Errorf("file_path missing: %v", args)
	}
	if _, has := args["path"]; has {
		t.Errorf("path must be renamed to schema key file_path: %v", args)
	}
}

func TestParseToolCallsMismatchedClosingTagAndCoercion(t *testing.T) {
	// deepseek-v4-pro leaked <tool_call> ... </tool_calls> (mismatched closing tag).
	in := `<tool_call> <invoke name="read"> <parameter name="file_path">C:\a.md</parameter> <parameter name="offset">130</parameter> <parameter name="limit">80</parameter> </invoke> </tool_calls>`
	_, calls := ParseToolCalls(in, readSchemas())
	if len(calls) != 1 {
		t.Fatalf("calls = %+v", calls)
	}
	args := decodeArgs(t, calls[0].Function.Arguments)
	if args["offset"] != float64(130) || args["limit"] != float64(80) {
		t.Errorf("integers not coerced: %v", args)
	}
	if args["file_path"] != `C:\a.md` {
		t.Errorf("file_path = %v", args["file_path"])
	}
}

func TestParseToolCallsSchemaRenamesFilePathToPath(t *testing.T) {
	schemas := toolSchemas{
		"read": &ToolSpec{
			Properties: map[string]string{"path": "string"},
			Required:   map[string]bool{"path": true},
		},
	}
	in := `<tool_calls><invoke name="Read"><parameter name="file_path">x.md</parameter></invoke></tool_calls>`
	_, calls := ParseToolCalls(in, schemas)
	if len(calls) != 1 || calls[0].Function.Name != "read" {
		t.Fatalf("calls = %+v", calls)
	}
	args := decodeArgs(t, calls[0].Function.Arguments)
	if args["path"] != "x.md" || len(args) != 1 {
		t.Errorf("args = %v", args)
	}
}

func TestParseToolCallsTruncatedAndMultiple(t *testing.T) {
	in := `<｜DSML｜function_calls><｜DSML｜invoke name="grep"><｜DSML｜parameter name="pattern" string="true">flow</｜DSML｜parameter></｜DSML｜invoke>` +
		`<｜DSML｜invoke name="read"><｜DSML｜parameter name="path" string="true">a.md</｜DSML｜parameter>`
	_, calls := ParseToolCalls(in, nil)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %+v", calls)
	}
	if calls[0].Index != 0 || calls[1].Index != 1 {
		t.Errorf("indexes: %d %d", calls[0].Index, calls[1].Index)
	}
	if calls[0].Function.Name != "grep" || calls[1].Function.Name != "read" {
		t.Errorf("names: %s %s", calls[0].Function.Name, calls[1].Function.Name)
	}
	args := decodeArgs(t, calls[1].Function.Arguments)
	if args["path"] != "a.md" || args["file_path"] != "a.md" {
		t.Errorf("unknown schema should mirror aliases: %v", args)
	}
}

func TestParseToolCallsJSONStyle(t *testing.T) {
	in := "ok\n<tool_call>\n{\"name\": \"read\", \"arguments\": {\"path\": \"a.md\", \"limit\": 5}}\n</tool_call>"
	clean, calls := ParseToolCalls(in, readSchemas())
	if clean != "ok" || len(calls) != 1 {
		t.Fatalf("clean=%q calls=%+v", clean, calls)
	}
	args := decodeArgs(t, calls[0].Function.Arguments)
	if args["file_path"] != "a.md" || args["limit"] != float64(5) {
		t.Errorf("args = %v", args)
	}
}

func TestParseToolCallsPlainTextUntouched(t *testing.T) {
	for _, in := range []string{
		"Gunakan tag <div> dan <b>tebal</b> untuk HTML.",
		"if a < b and tool_call is a name, nothing happens",
		"",
	} {
		clean, calls := ParseToolCalls(in, nil)
		if clean != in || calls != nil {
			t.Errorf("input %q modified: clean=%q calls=%v", in, clean, calls)
		}
	}
}

func TestExtractToolSchemas(t *testing.T) {
	var body map[string]any
	json.Unmarshal([]byte(`{"tools":[{"type":"function","function":{"name":"read","parameters":{"type":"object","properties":{"file_path":{"type":"string"},"offset":{"type":"integer"}}}}}]}`), &body)
	s := extractToolSchemas(body)
	if s["read"].PropType("file_path") != "string" || s["read"].PropType("offset") != "integer" {
		t.Errorf("schemas = %v", s)
	}
	if extractToolSchemas(map[string]any{}) != nil {
		t.Errorf("expected nil schemas when no tools")
	}
}

func feedAll(f *toolMarkupFilter, parts ...string) (string, []ToolCall) {
	var sb strings.Builder
	for _, p := range parts {
		sb.WriteString(f.Push(p))
	}
	left, calls := f.Finish()
	sb.WriteString(left)
	return sb.String(), calls
}

func TestToolMarkupFilterSplitMarkerAcrossChunks(t *testing.T) {
	full := `Saya baca dulu. <tool_call><invoke name="read"><parameter name="path">a.md</parameter></invoke></tool_call>`
	// split at every possible position: marker must never leak and text must be complete.
	for i := 1; i < len(full); i++ {
		f := newToolMarkupFilter(readSchemas())
		text, calls := feedAll(f, full[:i], full[i:])
		if strings.Contains(text, "<") || strings.Contains(text, "invoke") {
			t.Fatalf("split %d leaked markup: %q", i, text)
		}
		if strings.TrimSpace(text) != "Saya baca dulu." {
			t.Fatalf("split %d text = %q", i, text)
		}
		if len(calls) != 1 {
			t.Fatalf("split %d calls = %+v", i, calls)
		}
	}
}

func TestToolMarkupFilterDSMLSplitCharByChar(t *testing.T) {
	full := "Halo. < | | DSML | | calls> < | | DSML | | invoke name=\"read\"> < | | DSML | | parameter name=\"path\" string=\"true\">a.md</ | | DSML | | parameter> </ | | DSML | | invoke> </ | | DSML | | calls>"
	f := newToolMarkupFilter(nil)
	var sb strings.Builder
	for _, r := range full {
		sb.WriteString(f.Push(string(r)))
	}
	left, calls := f.Finish()
	sb.WriteString(left)
	if strings.TrimSpace(sb.String()) != "Halo." {
		t.Errorf("text = %q", sb.String())
	}
	if len(calls) != 1 || calls[0].Function.Name != "read" {
		t.Errorf("calls = %+v", calls)
	}
}

func TestToolMarkupFilterKeepsNormalAngleBrackets(t *testing.T) {
	in := "Contoh: a < b, <div>x</div>, dan List<String> serta <toolbox>."
	f := newToolMarkupFilter(nil)
	text, calls := feedAll(f, in[:10], in[10:25], in[25:])
	if text != in || calls != nil {
		t.Errorf("text = %q calls=%v", text, calls)
	}
}

func TestToolMarkupFilterUnparseableMarkerFallsBackToText(t *testing.T) {
	f := newToolMarkupFilter(nil)
	text, calls := feedAll(f, "intro <tool_call> bukan json </tool_call>")
	if calls != nil || !strings.Contains(text, "bukan json") {
		t.Errorf("text = %q calls=%v", text, calls)
	}
}

func TestStreamPuterSSEWithToolParsing(t *testing.T) {
	upstream := strings.Join([]string{
		`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"claude-haiku-5-5","choices":[{"index":0,"delta":{"role":"assistant","content":"Saya baca dulu.\n<tool"},"finish_reason":null}]}`,
		``,
		`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"claude-haiku-5-5","choices":[{"index":0,"delta":{"content":"_call> <invoke name=\"read\"> <parameter name=\"path\">a.md</parameter> </invoke> </tool_call>"},"finish_reason":null}]}`,
		``,
		`data: {"id":"c1","object":"chat.completion.chunk","created":1,"model":"claude-haiku-5-5","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		``,
		`data: [DONE]`,
		``,
	}, "\n")

	rec := httptest.NewRecorder()
	streamPuterSSEWithToolParsing(strings.NewReader(upstream), rec, readSchemas())
	body := rec.Body.String()

	if strings.Contains(body, "<tool") || strings.Contains(body, "invoke") {
		t.Errorf("markup leaked:\n%s", body)
	}
	if !strings.Contains(body, `"finish_reason":"tool_calls"`) {
		t.Errorf("missing finish_reason tool_calls:\n%s", body)
	}
	if strings.Contains(body, `"finish_reason":"stop"`) {
		t.Errorf("stop finish_reason must be replaced:\n%s", body)
	}
	if !strings.Contains(body, `"index":0`) || !strings.Contains(body, `"name":"read"`) || !strings.Contains(body, `file_path`) {
		t.Errorf("tool call payload missing:\n%s", body)
	}
	if !strings.HasSuffix(body, "data: [DONE]\n\n") || strings.Count(body, "[DONE]") != 1 {
		t.Errorf("stream must end with a single [DONE]:\n%s", body)
	}
	if !strings.Contains(body, "Saya baca dulu.") {
		t.Errorf("leading text lost:\n%s", body)
	}
}

func TestStreamPuterSSEPassThroughPlainAndSynthesizesFinish(t *testing.T) {
	// upstream ends without finish_reason / [DONE]: proxy must terminate the stream properly.
	upstream := `data: {"id":"c2","object":"chat.completion.chunk","created":2,"model":"m","choices":[{"index":0,"delta":{"role":"assistant","content":"Halo"},"finish_reason":null}]}` + "\n\n"
	rec := httptest.NewRecorder()
	streamPuterSSEWithToolParsing(strings.NewReader(upstream), rec, nil)
	body := rec.Body.String()
	if !strings.Contains(body, `"content":"Halo"`) || !strings.Contains(body, `"finish_reason":"stop"`) || !strings.HasSuffix(body, "data: [DONE]\n\n") {
		t.Errorf("unexpected body:\n%s", body)
	}
}

func TestRewriteChatCompletionToolCalls(t *testing.T) {
	in := `{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"cek\n<tool_call><invoke name=\"read\"><parameter name=\"path\">a.md</parameter></invoke></tool_call>"},"finish_reason":"stop"}]}`
	out := rewriteChatCompletionToolCalls([]byte(in), readSchemas())
	m := map[string]any{}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatalf("invalid json: %v", err)
	}
	choice := m["choices"].([]any)[0].(map[string]any)
	if choice["finish_reason"] != "tool_calls" {
		t.Errorf("finish_reason = %v", choice["finish_reason"])
	}
	msg := choice["message"].(map[string]any)
	if msg["content"] != "cek" {
		t.Errorf("content = %v", msg["content"])
	}
	if tcs, _ := msg["tool_calls"].([]any); len(tcs) != 1 {
		t.Errorf("tool_calls = %v", msg["tool_calls"])
	}

	plain := `{"choices":[{"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}]}`
	if got := rewriteChatCompletionToolCalls([]byte(plain), nil); string(got) != plain {
		t.Errorf("plain response modified: %s", got)
	}
}

