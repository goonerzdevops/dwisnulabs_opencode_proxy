package proxy

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseDSMLToolCalls(t *testing.T) {
	input := `Saya akan periksa dulu file md-nya, lalu jawab penyebab diagram tidak tergenerate.

< | | DSML | | calls> < | | DSML | | invoke name="read"> < | | DSML | | parameter name="path" string="true">C:\D\Project\Microservices\TECH_STACK_BLUEPRINT_2026.md</ | | DSML | | parameter> < | | DSML | | parameter name="offset" string="false">130</ | | DSML | | parameter> < | | DSML | | parameter name="limit" string="false">80</ | | DSML | | parameter> </ | | DSML | | invoke> </ | | DSML | | calls>`

	if !HasDSML(input) {
		t.Fatalf("expected HasDSML to be true")
	}

	cleanText, calls := ParseDSMLToolCalls(input)

	if cleanText != "Saya akan periksa dulu file md-nya, lalu jawab penyebab diagram tidak tergenerate." {
		t.Errorf("unexpected cleanText: %q", cleanText)
	}

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}

	call := calls[0]
	if call.Function.Name != "read" {
		t.Errorf("expected func name 'read', got %q", call.Function.Name)
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		t.Fatalf("failed to unmarshal args: %v", err)
	}

	if args["path"] != `C:\D\Project\Microservices\TECH_STACK_BLUEPRINT_2026.md` {
		t.Errorf("unexpected path: %v", args["path"])
	}
	if args["file_path"] != `C:\D\Project\Microservices\TECH_STACK_BLUEPRINT_2026.md` {
		t.Errorf("expected normalized file_path: %v", args["file_path"])
	}
	if args["offset"] != float64(130) {
		t.Errorf("unexpected offset: %v", args["offset"])
	}
	if args["limit"] != float64(80) {
		t.Errorf("unexpected limit: %v", args["limit"])
	}
}

func TestParseDSMLToolFallback(t *testing.T) {
	input := `< | DSML | calls> TOOL: {"file_path": "C:\\doc.md", "offset": 175, "limit": 30}`
	if !HasDSML(input) {
		t.Fatalf("expected HasDSML to be true")
	}

	clean, calls := ParseDSMLToolCalls(input)
	if clean != "" {
		t.Errorf("expected empty clean, got %q", clean)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}
	if calls[0].Function.Name != "read" {
		t.Errorf("expected read, got %s", calls[0].Function.Name)
	}

	var args map[string]any
	json.Unmarshal([]byte(calls[0].Function.Arguments), &args)
	if args["file_path"] != `C:\doc.md` || args["path"] != `C:\doc.md` {
		t.Errorf("unexpected args: %v", args)
	}
}

func TestParseDSMLNoCalls(t *testing.T) {
	input := "Halo! Saya adalah DeepSeek model biasa."
	if HasDSML(input) {
		t.Fatalf("expected HasDSML to be false")
	}
	clean, calls := ParseDSMLToolCalls(input)
	if clean != input || len(calls) != 0 {
		t.Errorf("expected no modification, got clean: %q, calls: %v", clean, calls)
	}
}

func TestDeepSeekHarnessSubagentSynthesizesDescription(t *testing.T) {
	// DeepSeek Harness tool-subagent schema declares 'description' and 'prompt' as required.
	schemas := toolSchemas{
		"subagent": &ToolSpec{
			Properties: map[string]string{
				"prompt":      "string",
				"description": "string",
				"cwd":         "string",
				"name":        "string",
			},
			Required: map[string]bool{
				"prompt":      true,
				"description": true,
			},
		},
	}

	// Model outputs only 'prompt' without 'description' (the exact error from user screenshot).
	input := `<tool_call>
<invoke name="subagent">
<parameter name="prompt">Baca file berikut secara lengkap: C:\D\Project\Microservices\TECH_STACK_BLUEPRINT_2026.md
Setelah membaca, berikan ringkasan terstruktur dalam Bahasa Indonesia yang mencakup:
1. Tujuan dan ruang lingkup dokumen
2. Daftar teknologi/stack utama yang direkomendasikan</parameter>
</invoke>
</tool_call>`

	clean, calls := ParseToolCalls(input, schemas)
	if clean != "" {
		t.Errorf("expected empty clean text, got: %q", clean)
	}
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	call := calls[0]
	if call.Function.Name != "subagent" {
		t.Errorf("expected tool name 'subagent', got %q", call.Function.Name)
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
		t.Fatalf("failed to unmarshal args: %v", err)
	}

	// Check prompt
	if !strings.HasPrefix(args["prompt"].(string), "Baca file berikut secara lengkap") {
		t.Errorf("unexpected prompt: %v", args["prompt"])
	}

	// Check synthesized description
	desc, ok := args["description"].(string)
	if !ok || desc == "" {
		t.Fatalf("expected synthesized description to be present and non-empty: %v", args)
	}
	expectedDesc := `Baca file berikut secara lengkap: C:\D\Project\Microservices\TECH_STACK_BLUEPRINT_2026.md`
	if desc != expectedDesc {
		t.Errorf("expected description %q, got %q", expectedDesc, desc)
	}
}

func TestClineReadFilePreservesPathAndDoesNotInjectGarbage(t *testing.T) {
	// Cline extension schema for read_file declares 'path' as required.
	schemas := toolSchemas{
		"read_file": &ToolSpec{
			Properties: map[string]string{
				"path": "string",
			},
			Required: map[string]bool{
				"path": true,
			},
		},
	}

	// Model might emit 'file_path' instead of 'path'
	input := `<tool_call>
<invoke name="read_file">
<parameter name="file_path">src/main.ts</parameter>
</invoke>
</tool_call>`

	_, calls := ParseToolCalls(input, schemas)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("failed to unmarshal args: %v", err)
	}

	// 'path' must be populated
	if args["path"] != "src/main.ts" {
		t.Errorf("expected path to be 'src/main.ts', got %v", args["path"])
	}
	// 'file_path' must be deleted
	if _, exists := args["file_path"]; exists {
		t.Errorf("expected file_path to be removed when path is declared")
	}
	// 'description' must NOT be synthesized because Cline does not require it!
	if _, exists := args["description"]; exists {
		t.Errorf("description should NOT be injected when not required by Cline schema")
	}
}

func TestClineExecuteCommandNormalizesCmdToCommand(t *testing.T) {
	// Cline extension schema for execute_command declares 'command' as required.
	schemas := toolSchemas{
		"execute_command": &ToolSpec{
			Properties: map[string]string{
				"command": "string",
			},
			Required: map[string]bool{
				"command": true,
			},
		},
	}

	input := `<tool_call>
<invoke name="execute_command">
<parameter name="cmd">go test ./...</parameter>
</invoke>
</tool_call>`

	_, calls := ParseToolCalls(input, schemas)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("failed to unmarshal args: %v", err)
	}

	if args["command"] != "go test ./..." {
		t.Errorf("expected command 'go test ./...', got %v", args["command"])
	}
	if _, exists := args["cmd"]; exists {
		t.Errorf("cmd should be removed after normalizing to command")
	}
}

func TestDeepSeekHarnessReadNormalizesPathToFilePath(t *testing.T) {
	// DeepSeek Harness schema for 'read' declares 'file_path' as required.
	schemas := toolSchemas{
		"read": &ToolSpec{
			Properties: map[string]string{
				"file_path": "string",
				"offset":    "integer",
				"limit":     "integer",
			},
			Required: map[string]bool{
				"file_path": true,
			},
		},
	}

	input := `<tool_call>
<invoke name="read">
<parameter name="path">doc/plan.md</parameter>
<parameter name="offset">1</parameter>
<parameter name="limit">50</parameter>
</invoke>
</tool_call>`

	_, calls := ParseToolCalls(input, schemas)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call, got %d", len(calls))
	}

	var args map[string]any
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("failed to unmarshal args: %v", err)
	}

	if args["file_path"] != "doc/plan.md" {
		t.Errorf("expected file_path 'doc/plan.md', got %v", args["file_path"])
	}
	if _, exists := args["path"]; exists {
		t.Errorf("path should be removed after normalizing to file_path")
	}
	if args["offset"] != int64(1) && args["offset"] != float64(1) {
		t.Errorf("expected offset 1, got %v", args["offset"])
	}
}
