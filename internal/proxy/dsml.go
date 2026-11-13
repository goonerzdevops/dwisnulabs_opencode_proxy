package proxy

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// ToolCall represents an OpenAI-compatible function tool call.
type ToolCall struct {
	Index    int          `json:"index"`
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// ToolSpec describes the declared contract for a single tool in the client request.
type ToolSpec struct {
	Properties map[string]string // property name -> type ("string", "integer", "object", etc.)
	Required   map[string]bool   // property name -> true if declared required
}

// HasProperty reports whether key is a declared property.
func (s *ToolSpec) HasProperty(key string) bool {
	if s == nil || s.Properties == nil {
		return false
	}
	_, ok := s.Properties[key]
	return ok
}

// IsRequired reports whether key is required by the tool schema.
func (s *ToolSpec) IsRequired(key string) bool {
	if s == nil || s.Required == nil {
		return false
	}
	return s.Required[key]
}

// PropType returns the declared type of the property ("" if unknown).
func (s *ToolSpec) PropType(key string) string {
	if s == nil || s.Properties == nil {
		return ""
	}
	return s.Properties[key]
}

// toolSchemas maps declared tool name -> ToolSpec.
// It is built from the `tools` array of the incoming OpenAI request and lets the
// parser rename / coerce arguments and synthesize missing required fields so they satisfy
// whatever harness (DeepSeek Harness, Cline, etc.) is connected.
type toolSchemas map[string]*ToolSpec

const markupSep = `[\s|｜]*`

var (
	// Opening marker of a textual tool-call block. Covers:
	//   <｜DSML｜calls>, < | | DSML | | calls>, <｜DSML｜function_calls>   (DeepSeek DSML)
	//   <tool_call>, <tool_calls>, <function_calls>                          (Anthropic / Hermes style)
	//   <invoke name="...">, <| DSML | invoke name="...">                    (bare invoke blocks)
	toolMarkupStartRe = regexp.MustCompile(`(?i)<` + markupSep +
		`(?:DSML` + markupSep + `(?:tool_calls?|function_calls|calls)\s*>` +
		`|(?:tool_calls?|function_calls)\s*>` +
		`|(?:DSML` + markupSep + `)?invoke\s+name\s*=)`)

	toolInvokeRe = regexp.MustCompile(`(?i)<` + markupSep + `(?:DSML` + markupSep + `)?invoke\s+name\s*=\s*["']([^"']+)["']\s*>([\s\S]*?)(?:</` +
		markupSep + `(?:DSML` + markupSep + `)?invoke\s*>|$)`)

	toolParamRe = regexp.MustCompile(`(?i)<` + markupSep + `(?:DSML` + markupSep + `)?parameter\s+name\s*=\s*["']([^"']+)["']((?:\s+[\w-]+\s*=\s*["'][^"']*["'])*)\s*>([\s\S]*?)(?:</` +
		markupSep + `(?:DSML` + markupSep + `)?parameter\s*>|$)`)

	toolStringAttrRe = regexp.MustCompile(`(?i)\bstring\s*=\s*["'](true|false)["']`)

	// Hermes / Qwen style: <tool_call>{"name": "read", "arguments": {...}}</tool_call>
	toolJSONCallRe = regexp.MustCompile(`(?is)<` + markupSep + `tool_call\s*>\s*(\{[\s\S]*?\})\s*</` + markupSep + `tool_call\s*>`)

	// Fallback TOOL: {"file_path": ...} lines emitted when a model is confused.
	dsmlToolFallbackRe = regexp.MustCompile(`(?i)TOOL:\s*(\{[^}]+\})`)
)

// argument alias groups: the first name found in the client's declared schema wins.
var argAliasGroups = [][]string{
	{"file_path", "path", "filePath", "filepath", "file"},
	{"command", "cmd", "script"},
	{"query", "q"},
	{"prompt", "instruction", "task"},
}

// HasDSML reports whether text contains a textual (DSML / XML) tool call marker.
func HasDSML(text string) bool {
	return toolMarkupStartRe.MatchString(text)
}

// extractToolSchemas builds toolSchemas from an OpenAI style request body.
func extractToolSchemas(body map[string]any) toolSchemas {
	rawTools, ok := body["tools"].([]any)
	if !ok {
		// body may have been built with a typed slice
		if typed, ok2 := body["tools"].([]map[string]any); ok2 {
			rawTools = make([]any, len(typed))
			for i, t := range typed {
				rawTools[i] = t
			}
		} else {
			return nil
		}
	}
	out := toolSchemas{}
	for _, rt := range rawTools {
		tool, ok := rt.(map[string]any)
		if !ok {
			continue
		}
		spec := tool
		if fn, ok := tool["function"].(map[string]any); ok {
			spec = fn
		}
		name, _ := spec["name"].(string)
		if name == "" {
			continue
		}
		props := map[string]string{}
		reqMap := map[string]bool{}

		params, _ := spec["parameters"].(map[string]any)
		if params == nil {
			params, _ = spec["input_schema"].(map[string]any)
		}
		if params != nil {
			if p, ok := params["properties"].(map[string]any); ok {
				for pn, pv := range p {
					typ := ""
					if pm, ok := pv.(map[string]any); ok {
						typ, _ = pm["type"].(string)
					}
					props[pn] = typ
				}
			}
			if reqList, ok := params["required"].([]any); ok {
				for _, r := range reqList {
					if rs, ok := r.(string); ok && rs != "" {
						reqMap[rs] = true
					}
				}
			} else if reqListStr, ok := params["required"].([]string); ok {
				for _, rs := range reqListStr {
					if rs != "" {
						reqMap[rs] = true
					}
				}
			}
		}
		out[name] = &ToolSpec{
			Properties: props,
			Required:   reqMap,
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// resolveToolName maps an emitted tool name onto a declared tool (case-insensitive and format-tolerant).
func (s toolSchemas) resolveToolName(name string) string {
	if s == nil || len(s) == 0 {
		return name
	}
	if _, ok := s[name]; ok {
		return name
	}
	for declared := range s {
		if strings.EqualFold(declared, name) {
			return declared
		}
	}
	cleanEmitted := strings.ReplaceAll(strings.ToLower(name), "_", "")
	for declared := range s {
		cleanDeclared := strings.ReplaceAll(strings.ToLower(declared), "_", "")
		if cleanEmitted == cleanDeclared {
			return declared
		}
	}
	return name
}

// summarizePrompt extracts a concise summary (up to maxLen chars) from an instruction or prompt.
func summarizePrompt(text string, maxLen int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if idx := strings.IndexAny(text, "\r\n"); idx >= 0 {
		firstLine := strings.TrimSpace(text[:idx])
		if len(firstLine) > 0 && len(firstLine) <= maxLen {
			return firstLine
		}
		if len(firstLine) > maxLen {
			text = firstLine
		}
	}
	if len(text) > maxLen {
		cut := text[:maxLen]
		if lastSpace := strings.LastIndex(cut, " "); lastSpace > maxLen/3 {
			cut = cut[:lastSpace]
		}
		return strings.TrimSpace(cut)
	}
	return text
}

// resolvePropertyAliases renames argument aliases to match whatever the client schema declares.
// If the schema is unknown (nil or empty properties), it mirrors common aliases so either spelling is satisfied.
func resolvePropertyAliases(argMap map[string]any, spec *ToolSpec) {
	for _, group := range argAliasGroups {
		if spec != nil && len(spec.Properties) > 0 {
			for _, key := range group {
				val, present := argMap[key]
				if !present {
					continue
				}
				if spec.HasProperty(key) {
					continue
				}
				for _, alias := range group {
					if spec.HasProperty(alias) {
						if _, has := argMap[alias]; !has {
							argMap[alias] = val
							delete(argMap, key)
						}
						break
					}
				}
			}
			continue
		}
		// Unknown schema: mirror aliases so either spelling is satisfied.
		for _, key := range group {
			if val, present := argMap[key]; present {
				for _, alias := range group[:2] {
					if _, has := argMap[alias]; !has {
						argMap[alias] = val
					}
				}
				break
			}
		}
	}
}

// synthesizeRequiredProperties synthesizes missing required properties if and only if
// they are explicitly declared in the client's tool schema `required` list.
func synthesizeRequiredProperties(argMap map[string]any, spec *ToolSpec) {
	if spec == nil || len(spec.Required) == 0 {
		return
	}
	for reqKey := range spec.Required {
		val, exists := argMap[reqKey]
		if !exists || val == nil || val == "" {
			switch reqKey {
			case "description", "summary", "label":
				// Synthesize concise summary from prompt, task, command, or query
				if prompt, ok := argMap["prompt"].(string); ok && prompt != "" {
					argMap[reqKey] = summarizePrompt(prompt, 100)
				} else if task, ok := argMap["task"].(string); ok && task != "" {
					argMap[reqKey] = summarizePrompt(task, 100)
				} else if cmd, ok := argMap["command"].(string); ok && cmd != "" {
					argMap[reqKey] = summarizePrompt(cmd, 100)
				} else if q, ok := argMap["query"].(string); ok && q != "" {
					argMap[reqKey] = summarizePrompt(q, 100)
				} else {
					argMap[reqKey] = "Task execution"
				}
			case "prompt":
				if desc, ok := argMap["description"].(string); ok && desc != "" {
					argMap[reqKey] = desc
				} else if task, ok := argMap["task"].(string); ok && task != "" {
					argMap[reqKey] = task
				} else if inst, ok := argMap["instruction"].(string); ok && inst != "" {
					argMap[reqKey] = inst
				}
			}
		}
	}
}

// normalizeToolArguments makes argument names match the client's declared schema.
// With a known schema it renames only when the emitted key is not declared and an alias is;
// it then synthesizes missing required properties (e.g. description) if declared in schema.
func normalizeToolArguments(argMap map[string]any, spec *ToolSpec) {
	resolvePropertyAliases(argMap, spec)
	synthesizeRequiredProperties(argMap, spec)
}

// coerceArgument converts a textual parameter into the type declared by the schema.
func coerceArgument(val string, typ string) any {
	switch typ {
	case "integer":
		if n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64); err == nil {
			return n
		}
		if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil && f == float64(int64(f)) {
			return int64(f)
		}
	case "number":
		if f, err := strconv.ParseFloat(strings.TrimSpace(val), 64); err == nil {
			if f == float64(int64(f)) {
				return int64(f)
			}
			return f
		}
	case "boolean":
		switch strings.ToLower(strings.TrimSpace(val)) {
		case "true":
			return true
		case "false":
			return false
		}
	case "array", "object":
		var v any
		if err := json.Unmarshal([]byte(val), &v); err == nil {
			return v
		}
	}
	return val
}

func buildToolCall(index int, name string, args map[string]any, schemas toolSchemas) ToolCall {
	var spec *ToolSpec
	if schemas != nil {
		name = schemas.resolveToolName(name)
		spec = schemas[name]
	}
	normalizeToolArguments(args, spec)
	rawArgs, _ := json.Marshal(args)
	return ToolCall{
		Index: index,
		ID:    "call_" + randomBase62(12),
		Type:  "function",
		Function: ToolFunction{
			Name:      name,
			Arguments: string(rawArgs),
		},
	}
}

// ParseDSMLToolCalls extracts textual tool calls without schema knowledge.
func ParseDSMLToolCalls(rawText string) (string, []ToolCall) {
	return ParseToolCalls(rawText, nil)
}

// ParseToolCalls extracts DSML / XML / JSON textual tool calls. It returns the text that
// precedes the first marker and the parsed calls. When nothing could be parsed the
// original text is returned untouched together with nil calls.
func ParseToolCalls(rawText string, schemas toolSchemas) (string, []ToolCall) {
	loc := toolMarkupStartRe.FindStringIndex(rawText)
	if loc == nil {
		return rawText, nil
	}

	cleanText := strings.TrimSpace(rawText[:loc[0]])
	section := rawText[loc[0]:]

	var toolCalls []ToolCall

	// 1) <invoke name="..."><parameter name="...">...</parameter></invoke>
	for _, inv := range toolInvokeRe.FindAllStringSubmatch(section, -1) {
		funcName := strings.TrimSpace(inv[1])
		toolName := schemas.resolveToolName(funcName)
		var spec *ToolSpec
		if schemas != nil {
			spec = schemas[toolName]
		}

		argMap := make(map[string]any)
		for _, pm := range toolParamRe.FindAllStringSubmatch(inv[2], -1) {
			paramName := pm[1]
			paramVal := strings.TrimSpace(pm[3])

			if attr := toolStringAttrRe.FindStringSubmatch(pm[2]); attr != nil && strings.EqualFold(attr[1], "false") {
				if num, err := strconv.ParseFloat(paramVal, 64); err == nil {
					if num == float64(int64(num)) {
						argMap[paramName] = int64(num)
					} else {
						argMap[paramName] = num
					}
					continue
				}
				if paramVal == "true" {
					argMap[paramName] = true
					continue
				}
				if paramVal == "false" {
					argMap[paramName] = false
					continue
				}
				var v any
				if err := json.Unmarshal([]byte(paramVal), &v); err == nil {
					argMap[paramName] = v
					continue
				}
			}
			argMap[paramName] = paramVal
		}

		// Schema driven coercion (Anthropic style has no string="false" hint).
		// Must run after alias normalization so we look up the declared key.
		normalizeToolArguments(argMap, spec)
		if spec != nil {
			for k, v := range argMap {
				if sv, ok := v.(string); ok {
					if typ := spec.PropType(k); typ != "" && typ != "string" {
						argMap[k] = coerceArgument(sv, typ)
					}
				}
			}
		}
		toolCalls = append(toolCalls, buildToolCall(len(toolCalls), toolName, argMap, schemas))
	}

	// 2) <tool_call>{"name": ..., "arguments": {...}}</tool_call>
	if len(toolCalls) == 0 {
		for _, jm := range toolJSONCallRe.FindAllStringSubmatch(section, -1) {
			var obj struct {
				Name       string         `json:"name"`
				Arguments  map[string]any `json:"arguments"`
				Parameters map[string]any `json:"parameters"`
			}
			if err := json.Unmarshal([]byte(jm[1]), &obj); err != nil || obj.Name == "" {
				continue
			}
			args := obj.Arguments
			if args == nil {
				args = obj.Parameters
			}
			if args == nil {
				args = map[string]any{}
			}
			toolCalls = append(toolCalls, buildToolCall(len(toolCalls), obj.Name, args, schemas))
		}
	}

	// 3) TOOL: {...} fallback
	if len(toolCalls) == 0 {
		for _, fm := range dsmlToolFallbackRe.FindAllStringSubmatch(section, -1) {
			var argMap map[string]any
			if err := json.Unmarshal([]byte(fm[1]), &argMap); err != nil {
				continue
			}
			funcName := "read"
			if _, hasContent := argMap["content"]; hasContent {
				funcName = "write"
			}
			toolCalls = append(toolCalls, buildToolCall(len(toolCalls), funcName, argMap, schemas))
		}
	}

	if len(toolCalls) == 0 {
		return cleanText, nil
	}
	return cleanText, toolCalls
}
