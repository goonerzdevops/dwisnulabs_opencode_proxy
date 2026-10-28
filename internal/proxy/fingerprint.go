package proxy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
)

// Impersonation constants mirrored from the OpenCode v2 CLI. Upstream rejects
// the v1 "opencode/1.18.x" User-Agent with 403 FreeTierError since 2026-10-14.
const (
	UpstreamUserAgent = "opencode/latest/2.1.3/cli"
	UpstreamClient    = "cli"
	UpstreamAuth      = "Bearer public"
)

// UpstreamProject is the default project identifier matching OpenCode v2 requirements (hex40).
var UpstreamProject = GetDefaultProjectID()

// FingerprintToolNames are the tool names the free-tier gate requires to be
// declared verbatim by every request.
var FingerprintToolNames = []string{"read", "shell"}

// Raw verbatim definitions from the OpenCode v2.0.26 CLI. Upstream compares both
// description and parameters, so these strings must not be reworded, reordered
// or reformatted. Parsed through encoding/json so numeric fields land as float64
// exactly like a client-decoded body.
const fingerprintRawJSON = `{
  "read": {
    "description": "Read the contents of a file or directory. Supports text files, images, and PDFs. Images and PDFs are presented directly to the model. Each text line is prefixed by its 1-based line number as <line>: <content>. The prefix is for reference and is not part of the file content. Directory entries are returned one per line. Use offset and limit to read large files or directories in sections. Prefer one larger read over many small slices, and use grep to find specific content in large files.",
    "parameters": {
      "type": "object",
      "properties": {
        "path": { "type": "string", "description": "File or directory to read" },
        "offset": { "type": "number", "description": "The line or directory entry to start reading from (1-based)" },
        "limit": { "type": "number", "description": "The maximum number of lines or directory entries to read (defaults to and capped at 2000)" }
      },
      "required": ["path"]
    }
  },
  "shell": {
    "description": "Execute a shell command and return its output. Quote file paths containing spaces or special characters. Prefer dedicated tools over shell commands when possible. When output is large, the full result is saved to a file and a truncated preview is returned. Rely on automatic truncation unless filtering the output is more useful. Commands accept an optional timeout, background commands have no timeout by default. Background commands return immediately, and you will be notified when they complete. DO NOT poll for completion.",
    "parameters": {
      "type": "object",
      "properties": {
        "command": { "type": "string", "description": "Shell command string to execute" },
        "workdir": { "type": "string", "description": "Working directory to execute the command in. Defaults to the current working directory. When possible, avoid changing directories in the command and set the working directory here instead." },
        "timeout": { "type": "number", "description": "Timeout in milliseconds. Set to 0 to disable the timeout." },
        "background": { "type": "boolean", "description": "Run the command in the background and return immediately (useful for dev servers and long-running builds). You do not need to use '&' at the end of the command when using this parameter. You will be notified when it completes. DO NOT poll for completion." }
      },
      "required": ["command"]
    }
  }
}`

type fingerprintDefinition struct {
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

var fingerprintDefinitions = func() map[string]fingerprintDefinition {
	defs := make(map[string]fingerprintDefinition)
	if err := json.Unmarshal([]byte(fingerprintRawJSON), &defs); err != nil {
		panic("opencode-proxy: invalid embedded fingerprint definitions: " + err.Error())
	}
	for _, name := range FingerprintToolNames {
		if _, ok := defs[name]; !ok {
			panic("opencode-proxy: fingerprint definition missing for " + name)
		}
	}
	return defs
}()

// unavailableNote explains client-compatibility stub tools to the model.
const unavailableNote = "\n\nThe following tools exist only for client compatibility handshakes and are not available to you: %s. Do not call them."

// RenameMap tracks names that changed on the way upstream: sent name -> caller's
// original name. The response side reverses it.
type RenameMap map[string]string

// Restore maps an upstream name back to the caller's spelling.
func (r RenameMap) Restore(name string) string {
	if len(r) == 0 {
		return name
	}
	if original, ok := r[name]; ok {
		return original
	}
	return name
}

func (r RenameMap) reversed() map[string]string {
	out := make(map[string]string, len(r))
	for sent, original := range r {
		out[original] = sent
	}
	return out
}

// aliasToolName builds the temporary name a colliding caller tool travels under.
// Kept short and lowercase so it survives upstream tool-name validation.
func aliasToolName(original string, seq int) string {
	return fmt.Sprintf("hidden_%s_%d", strings.ToLower(original), seq)
}

// toolNameCarrier returns the name plus the map that physically holds it. Both
// wire shapes are supported: Chat Completions nesting ({function:{name}}) and
// Responses flattening ({name}).
func toolNameCarrier(tool map[string]any) (string, map[string]any) {
	if fn, ok := tool["function"].(map[string]any); ok {
		name, _ := fn["name"].(string)
		return name, fn
	}
	name, _ := tool["name"].(string)
	return name, tool
}

func fingerprintKey(name string) string {
	key := strings.ToLower(strings.TrimSpace(name))
	if _, ok := fingerprintDefinitions[key]; ok {
		return key
	}
	return ""
}

func matchesFingerprint(tool map[string]any, def fingerprintDefinition) bool {
	_, carrier := toolNameCarrier(tool)
	description, _ := carrier["description"].(string)
	if description != def.Description {
		return false
	}
	return reflect.DeepEqual(carrier["parameters"], def.Parameters)
}

// buildFingerprintTool renders the definition in the lane's wire shape.
func buildFingerprintTool(name string, def fingerprintDefinition, flat bool) map[string]any {
	if flat {
		return map[string]any{
			"type":        "function",
			"name":        name,
			"description": def.Description,
			"parameters":  def.Parameters,
		}
	}
	return map[string]any{
		"type": "function",
		"function": map[string]any{
			"name":        name,
			"description": def.Description,
			"parameters":  def.Parameters,
		},
	}
}

// ApplyFingerprintTools guarantees the free-tier gate sees the `read` and
// `shell` definitions verbatim. Caller tools travel unchanged, except a tool
// that already claims a fingerprint name with a different definition: it is
// renamed to an alias and the response side restores the caller's spelling.
// flat selects the Responses wire shape ({name}) over Chat Completions nesting
// ({function:{name}}).
func ApplyFingerprintTools(body map[string]any, flat bool) RenameMap {
	renames := RenameMap{}
	tools := concealFingerprintToolNames(body["tools"], renames)
	tools = appendMissingFingerprintTools(tools, flat)
	body["tools"] = tools

	retargetToolChoice(body, renames)
	aliasHistoryToolCalls(body, renames)

	if names := stillUnavailable(appendedToolNames(tools), renames); len(names) > 0 {
		noteUnavailableTools(body, flat, names)
	}

	// Upstream free gate requires the CLI <env> marker alongside fingerprint tools
	ApplyEnvFingerprint(body, flat)

	// Responses keeps an explicit "auto" default. Never emit "none": upstream
	// answers tool_choice "none" with 403 FreeTierError.
	if flat {
		if choice, ok := body["tool_choice"]; !ok || choice == nil || choice == "" || choice == "none" {
			body["tool_choice"] = "auto"
		}
	}
	return renames
}

// concealFingerprintToolNames renames colliding caller tools to aliases.
func concealFingerprintToolNames(raw any, renames RenameMap) []any {
	items, ok := raw.([]any)
	if !ok {
		return []any{}
	}
	out := make([]any, 0, len(items))
	seq := 0
	for _, item := range items {
		tool, ok := item.(map[string]any)
		if !ok {
			out = append(out, item)
			continue
		}
		name, carrier := toolNameCarrier(tool)
		key := fingerprintKey(name)
		if key == "" || matchesFingerprint(tool, fingerprintDefinitions[key]) {
			out = append(out, tool)
			continue
		}
		seq++
		alias := aliasToolName(name, seq)
		carrier["name"] = alias
		renames[alias] = name
		out = append(out, tool)
	}
	return out
}

// appendMissingFingerprintTools adds any fingerprint definition the request is
// still missing. The gate demands both names on every call, including calls that
// already carry a full agent toolset.
func appendMissingFingerprintTools(tools []any, flat bool) []any {
	present := map[string]bool{}
	for _, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := toolNameCarrier(tool)
		key := fingerprintKey(name)
		if key != "" && matchesFingerprint(tool, fingerprintDefinitions[key]) {
			present[key] = true
		}
	}
	for _, name := range FingerprintToolNames {
		if present[name] {
			continue
		}
		tools = append(tools, buildFingerprintTool(name, fingerprintDefinitions[name], flat))
	}
	return tools
}

func appendedToolNames(tools []any) []string {
	var names []string
	for _, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := toolNameCarrier(tool)
		if key := fingerprintKey(name); key != "" {
			names = append(names, key)
		}
	}
	return names
}

// stillUnavailable drops names that the caller really did supply (they merely
// travelled under an alias), so the model is not told to avoid a usable tool.
func stillUnavailable(appended []string, renames RenameMap) []string {
	concealed := map[string]bool{}
	for _, original := range renames {
		concealed[strings.ToLower(original)] = true
	}
	var out []string
	for _, name := range appended {
		if !concealed[name] {
			out = append(out, name)
		}
	}
	return out
}

// retargetToolChoice points an explicit tool_choice at the alias name.
func retargetToolChoice(body map[string]any, renames RenameMap) {
	if len(renames) == 0 {
		return
	}
	reverse := renames.reversed()

	switch choice := body["tool_choice"].(type) {
	case string:
		if choice == "none" {
			body["tool_choice"] = "auto"
		}
	case map[string]any:
		if fn, ok := choice["function"].(map[string]any); ok {
			if name, _ := fn["name"].(string); reverse[name] != "" {
				fn["name"] = reverse[name]
			}
			return
		}
		if name, ok := choice["name"].(string); ok && reverse[name] != "" {
			choice["name"] = reverse[name]
		}
	}
}

// aliasHistoryToolCalls rewrites prior assistant tool calls so the model sees the
// same names it was offered in `tools`.
func aliasHistoryToolCalls(body map[string]any, renames RenameMap) {
	if len(renames) == 0 {
		return
	}
	reverse := renames.reversed()

	if messages, ok := body["messages"].([]any); ok {
		for _, raw := range messages {
			message, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			calls, ok := message["tool_calls"].([]any)
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
				if name, _ := fn["name"].(string); reverse[name] != "" {
					fn["name"] = reverse[name]
				}
			}
		}
	}

	if input, ok := body["input"].([]any); ok {
		for _, raw := range input {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if kind, _ := item["type"].(string); kind != "function_call" {
				continue
			}
			if name, _ := item["name"].(string); reverse[name] != "" {
				item["name"] = reverse[name]
			}
		}
	}
}

// noteUnavailableTools tells the model the stubs are handshake-only.
func noteUnavailableTools(body map[string]any, flat bool, names []string) {
	note := fmt.Sprintf(unavailableNote, strings.Join(names, ", "))
	inline := strings.TrimPrefix(note, "\n\n")

	if flat {
		if text, ok := body["instructions"].(string); ok {
			body["instructions"] = text + note
			return
		}
		body["instructions"] = inline
		return
	}

	messages, _ := body["messages"].([]any)
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if role, _ := message["role"].(string); role != "system" && role != "developer" {
			continue
		}
		switch content := message["content"].(type) {
		case string:
			message["content"] = content + note
		case []any:
			message["content"] = append(content, map[string]any{"type": "text", "text": inline})
		default:
			continue
		}
		body["messages"] = messages
		return
	}
	body["messages"] = append([]any{map[string]any{"role": "system", "content": inline}}, messages...)
}

const (
	envTag     = "<env>"
	envHeading = "Here is some useful information about the environment you are running in:"
)

// BuildEnvFingerprint constructs the verbatim CLI <env> marker block.
func BuildEnvFingerprint() string {
	cwd, err := os.Getwd()
	if err != nil || cwd == "" {
		cwd = "C:\\D\\Project\\Mix\\opencode_proxy"
	}
	tmpDir := filepath.Join(os.TempDir(), "opencode")
	platform := runtime.GOOS
	if platform == "windows" {
		platform = "win32"
	}

	return fmt.Sprintf("%s\n%s\n  Working directory: %s\n  Workspace root folder: %s\n  Is directory a git repo: no\n  Platform: %s\n  Prefer %s over generic system temporary directories such as /tmp; it is pre-created and approved for external access.\n</env>",
		envHeading, envTag, cwd, cwd, platform, tmpDir)
}

// ApplyEnvFingerprint ensures the required CLI <env> marker is injected into the system prompt.
// Upstream answers 429 FreeUsageLimitError if this marker is missing or if a 3rd-party
// <env> block (such as Cline's) precedes or hijacks the authentic OpenCode CLI environment block.
func ApplyEnvFingerprint(body map[string]any, flat bool) {
	prompt := BuildEnvFingerprint()

	if flat {
		if text, ok := body["instructions"].(string); ok && text != "" {
			if !strings.Contains(text, envHeading) {
				text = strings.ReplaceAll(text, "<env>", "<environment_info>")
				text = strings.ReplaceAll(text, "</env>", "</environment_info>")
				body["instructions"] = prompt + "\n\n" + text
			}
			return
		}
		body["instructions"] = prompt
		return
	}

	messages, _ := body["messages"].([]any)
	for _, raw := range messages {
		message, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		role, _ := message["role"].(string)
		if role != "system" && role != "developer" {
			continue
		}
		switch content := message["content"].(type) {
		case string:
			if !strings.Contains(content, envHeading) {
				cleanContent := strings.ReplaceAll(content, "<env>", "<environment_info>")
				cleanContent = strings.ReplaceAll(cleanContent, "</env>", "</environment_info>")
				message["content"] = prompt + "\n\n" + cleanContent
			}
		case []any:
			hasHeading := false
			for _, item := range content {
				if m, ok := item.(map[string]any); ok {
					if t, ok := m["text"].(string); ok && strings.Contains(t, envHeading) {
						hasHeading = true
						break
					}
				}
			}
			if !hasHeading {
				message["content"] = append([]any{map[string]any{"type": "text", "text": prompt + "\n\n"}}, content...)
			}
		default:
			continue
		}
		body["messages"] = messages
		return
	}

	body["messages"] = append([]any{map[string]any{"role": "system", "content": prompt}}, messages...)
}
