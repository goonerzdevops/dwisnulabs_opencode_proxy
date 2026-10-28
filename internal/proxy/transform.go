package proxy

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"opencode-proxy/internal/config"
)

// Lane is the upstream API surface a model is served from.
type Lane string

// Lanes supported by the OpenCode free tier.
const (
	LaneChat      Lane = "chat"
	LaneResponses Lane = "responses"
	LaneMessages  Lane = "messages"
)

// responsesModels are served by /inference/v1/responses, not /chat/completions.
var responsesModels = map[string]bool{
	"muse-spark-1.2-contributor-free": true,
	"muse-spark-1.3-contributor-free": true,
}

// messagesModels are served by /inference/v1/messages (Anthropic wire format).
var messagesModels = map[string]bool{
	"union-alpha": true,
}

// forceAutoToolChoiceModels reject an explicit tool_choice: upstream answers
// anything but "auto" with 403 FreeTierError.
var forceAutoToolChoiceModels = map[string]bool{
	"muse-spark-1.3-contributor-free": true,
}

// IsResponsesModel reports whether model is served through the Responses API.
func IsResponsesModel(model string) bool { return responsesModels[strings.TrimSpace(model)] }

// IsMessagesModel reports whether model is served through the Anthropic API.
func IsMessagesModel(model string) bool { return messagesModels[strings.TrimSpace(model)] }

// LaneForModel maps a model id onto its upstream lane.
func LaneForModel(model string) Lane {
	switch model = strings.TrimSpace(model); {
	case responsesModels[model] || strings.Contains(model, "muse-spark"):
		return LaneResponses
	case messagesModels[model]:
		return LaneMessages
	default:
		return LaneChat
	}
}

// ResolveModel applies alias and default fallbacks and returns the upstream id.
func ResolveModel(cfg config.Config, requested string) string {
	model := strings.TrimSpace(requested)
	if model == "" || strings.EqualFold(model, "default") {
		return cfg.DefaultModel
	}
	if target, ok := cfg.ModelAliases[strings.ToLower(model)]; ok && target != "" {
		return target
	}
	return model
}

// UpstreamPath returns the upstream path for a lane. The free tier moved from
// /zen/v1 to /inference/v1; /zen/v1 now answers 403 FreeTierError.
func UpstreamPath(lane Lane) string {
	switch lane {
	case LaneResponses:
		return "/inference/openai/v1/responses"
	case LaneMessages:
		return "/inference/v1/messages"
	default:
		return "/inference/openai/v1/chat/completions"
	}
}

// PreparedRequest is a request ready to be posted upstream.
type PreparedRequest struct {
	Lane          Lane
	Model         string
	Path          string
	Body          map[string]any
	Renames       RenameMap
	ClientStreams bool
	DroppedFields []string
}

// chatOnlyFields have no Responses API equivalent and would be rejected.
var chatOnlyFields = []string{
	"stream_options", "n", "logprobs", "top_logprobs", "logit_bias",
	"frequency_penalty", "presence_penalty", "response_format", "seed",
}

// Prepare normalises an OpenAI Chat Completions body for the free tier:
// lane translation, fingerprint tools, forced upstream streaming.
func Prepare(cfg config.Config, in map[string]any) (*PreparedRequest, error) {
	body := make(map[string]any, len(in)+8)
	for k, v := range in {
		body[k] = v
	}

	model := ResolveModel(cfg, stringField(body, "model"))
	lane := LaneForModel(model)
	if lane == LaneMessages {
		return nil, fmt.Errorf("model %q is served on the Anthropic /v1/messages endpoint, which this proxy does not expose", model)
	}

	req := &PreparedRequest{
		Lane:    lane,
		Model:   model,
		Path:    UpstreamPath(lane),
		Body:    body,
		Renames: RenameMap{},
	}
	body["model"] = model

	req.ClientStreams, _ = body["stream"].(bool)
	// Always stream upstream; aggregate when the client asked for a plain body.
	body["stream"] = true

	if lane == LaneResponses {
		if err := prepareResponsesBody(body, model, req); err != nil {
			return nil, err
		}
		if tuning := strings.TrimSpace(cfg.SystemPromptTuning); tuning != "" {
			if existing := stringField(body, "instructions"); existing != "" {
				body["instructions"] = existing + "\n\n" + tuning
			} else {
				body["instructions"] = tuning
			}
		}
	} else {
		normalizeChatBody(body)
		applySystemPromptTuning(body, cfg.SystemPromptTuning)
		req.Renames = ApplyFingerprintTools(body, false)
	}
	return req, nil
}

func stringField(body map[string]any, key string) string {
	value, _ := body[key].(string)
	return value
}

// normalizeChatBody strips fields the upstream chat endpoint rejects and
// ensures compatibility with reasoning models (e.g. DeepSeek).
func normalizeChatBody(body map[string]any) {
	delete(body, "stream_options")
	ensureReasoningContent(body)
}

// ensureReasoningContent ensures every assistant message has reasoning_content populated.
// Upstream DeepSeek thinking models throw:
//
//	[invalid_request_error] The `reasoning_content` in the thinking mode must be passed back to the API.
//
// when multi-turn tool-calling histories omit reasoning_content (standard OpenAI clients like Cline do not persist it).
func ensureReasoningContent(body map[string]any) {
	raw, ok := body["messages"].([]any)
	if !ok || len(raw) == 0 {
		return
	}
	for _, m := range raw {
		msg, ok := m.(map[string]any)
		if !ok {
			continue
		}
		role, _ := msg["role"].(string)
		if strings.EqualFold(role, "assistant") {
			if _, exists := msg["reasoning_content"]; !exists || msg["reasoning_content"] == nil {
				msg["reasoning_content"] = ""
			}
		}
	}
}

func applySystemPromptTuning(body map[string]any, tuning string) {
	tuning = strings.TrimSpace(tuning)
	if tuning == "" {
		return
	}
	raw, ok := body["messages"].([]any)
	if !ok || len(raw) == 0 {
		return
	}

	if first, ok := raw[0].(map[string]any); ok {
		if role, _ := first["role"].(string); strings.EqualFold(role, "system") {
			content, _ := first["content"].(string)
			if !strings.Contains(content, tuning) {
				first["content"] = content + "\n\n" + tuning
			}
			return
		}
	}

	newMsg := map[string]any{
		"role":    "system",
		"content": tuning,
	}
	newMsgs := make([]any, 0, len(raw)+1)
	newMsgs = append(newMsgs, newMsg)
	newMsgs = append(newMsgs, raw...)
	body["messages"] = newMsgs
}

// prepareResponsesBody performs the Chat -> Responses translation the free tier
// requires, before the fingerprint is applied.
func prepareResponsesBody(body map[string]any, model string, req *PreparedRequest) error {
	if rawInput, ok := body["input"]; ok {
		items, ok := rawInput.([]any)
		if !ok {
			return fmt.Errorf("input must be an array of Responses items")
		}
		normalized, err := normalizeResponsesItems(items)
		if err != nil {
			return err
		}
		body["input"] = normalized
	} else {
		messages, ok := body["messages"].([]any)
		if !ok || len(messages) == 0 {
			return fmt.Errorf("messages must be a non-empty array")
		}
		input, instructions, err := messagesToResponsesInput(messages)
		if err != nil {
			return err
		}
		body["input"] = input
		if instructions != "" {
			if existing := stringField(body, "instructions"); existing != "" {
				body["instructions"] = existing + "\n\n" + instructions
			} else {
				body["instructions"] = instructions
			}
		}
	}
	delete(body, "messages")

	if _, ok := body["max_output_tokens"]; !ok {
		if value, ok := body["max_completion_tokens"]; ok {
			body["max_output_tokens"] = value
		} else if value, ok := body["max_tokens"]; ok {
			body["max_output_tokens"] = value
		}
	}
	delete(body, "max_tokens")
	delete(body, "max_completion_tokens")

	normalizeReasoning(body)
	body["store"] = false

	convertToolsToResponses(body)

	for _, field := range chatOnlyFields {
		if _, ok := body[field]; ok {
			delete(body, field)
			req.DroppedFields = append(req.DroppedFields, field)
		}
	}

	if forceAutoToolChoiceModels[model] {
		if choice, ok := body["tool_choice"].(string); ok && choice != "auto" {
			body["tool_choice"] = "auto"
		}
	}

	req.Renames = ApplyFingerprintTools(body, true)
	return nil
}

// convertToolsToResponses flattens Chat tools into Responses tools.
func convertToolsToResponses(body map[string]any) {
	raw, ok := body["tools"].([]any)
	if !ok {
		return
	}
	out := make([]any, 0, len(raw))
	for _, item := range raw {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := tool["function"].(map[string]any)
		if !ok {
			// Already flat, or a built-in tool such as web_search.
			out = append(out, tool)
			continue
		}
		flat := map[string]any{"type": "function"}
		for key, value := range fn {
			flat[key] = value
		}
		out = append(out, flat)
	}
	body["tools"] = out
}

// normalizeReasoning moves Anthropic-style thinking onto the Responses
// reasoning object; chat-shaped reasoning fields are dropped.
func normalizeReasoning(body map[string]any) {
	if thinking, ok := body["thinking"].(map[string]any); ok {
		if kind, _ := thinking["type"].(string); kind == "enabled" {
			if _, exists := body["reasoning"]; !exists {
				if budget := numberField(thinking, "budget_tokens"); budget > 0 {
					body["reasoning"] = map[string]any{"effort": effortForBudget(budget)}
				}
			}
		}
	}
	delete(body, "thinking")

	reasoning, ok := body["reasoning"].(map[string]any)
	if !ok {
		if body["reasoning"] != nil {
			delete(body, "reasoning")
		}
		return
	}
	clean := map[string]any{}
	if effort, ok := reasoning["effort"].(string); ok && effort != "" && effort != "none" {
		clean["effort"] = effort
	}
	if summary, ok := reasoning["summary"].(string); ok && summary != "" {
		clean["summary"] = summary
	}
	if len(clean) == 0 {
		delete(body, "reasoning")
		return
	}
	body["reasoning"] = clean
}

func effortForBudget(budget float64) string {
	switch {
	case budget <= 0:
		return "low"
	case budget <= 4096:
		return "low"
	case budget <= 16384:
		return "medium"
	default:
		return "high"
	}
}

func numberField(body map[string]any, key string) float64 {
	switch value := body[key].(type) {
	case float64:
		return value
	case int:
		return float64(value)
	case json.Number:
		f, _ := value.Float64()
		return f
	default:
		return 0
	}
}

func marshalJSON(value any) string {
	if value == nil {
		return "{}"
	}
	if text, ok := value.(string); ok {
		return text
	}
	blob, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(blob)
}

// clampCallID keeps tool call ids inside the Responses limit deterministically,
// so a client can still match its own ids.
func clampCallID(id string) string {
	const limit = 64
	id = strings.TrimSpace(id)
	if id == "" {
		return "call_" + randomBase62(12)
	}
	if len(id) <= limit {
		return id
	}
	sum := sha1.Sum([]byte(id))
	suffix := hex.EncodeToString(sum[:4])
	return id[:limit-len(suffix)] + suffix
}
