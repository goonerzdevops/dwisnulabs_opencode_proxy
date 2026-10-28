package proxy

import (
	"net/http"
	"regexp"
	"testing"

	"opencode-proxy/internal/config"
)

var (
	hex40Pattern       = regexp.MustCompile(`^[0-9a-f]{40}$`)
	b3Pattern          = regexp.MustCompile(`^[0-9a-f]{32}-[0-9a-f]{16}-1-[0-9a-f]{16}$`)
	traceparentPattern = regexp.MustCompile(`^00-[0-9a-f]{32}-[0-9a-f]{16}-01$`)
)

func TestZenV2UpstreamPaths(t *testing.T) {
	if got := UpstreamPath(LaneChat); got != "/inference/openai/v1/chat/completions" {
		t.Fatalf("LaneChat path = %q, want /inference/openai/v1/chat/completions", got)
	}
	if got := UpstreamPath(LaneResponses); got != "/inference/openai/v1/responses" {
		t.Fatalf("LaneResponses path = %q, want /inference/openai/v1/responses", got)
	}
	if got := UpstreamPath(LaneMessages); got != "/inference/v1/messages" {
		t.Fatalf("LaneMessages path = %q, want /inference/v1/messages", got)
	}
}

func TestZenV2LaneForModel(t *testing.T) {
	cases := []struct {
		model    string
		expected Lane
	}{
		{"big-pickle", LaneChat},
		{"space-bunny-free", LaneChat},
		{"cat-jam-free", LaneChat},
		{"mimo-v2.6-flash-free", LaneChat},
		{"nemotron-3.5-lightning-free", LaneChat},
		{"muse-spark-1.2-contributor-free", LaneResponses},
		{"muse-spark-1.3-contributor-free", LaneResponses},
		{"custom-muse-spark-test", LaneResponses},
		{"union-alpha", LaneMessages},
	}

	for _, c := range cases {
		if got := LaneForModel(c.model); got != c.expected {
			t.Errorf("LaneForModel(%q) = %v, want %v", c.model, got, c.expected)
		}
	}
}

func TestZenV2HeadersContract(t *testing.T) {
	up := NewUpstream(config.Default(), nil)
	header := http.Header{}
	up.applyHeaders(header, true, Fingerprint{SessionID: GenerateSessionID(1730000000000)})

	// 1. User-Agent
	if got := header.Get("User-Agent"); got != "opencode/latest/2.1.3/cli" {
		t.Errorf("User-Agent = %q, want opencode/latest/2.1.3/cli", got)
	}

	// 2. Authorization
	if got := header.Get("Authorization"); got != "Bearer public" {
		t.Errorf("Authorization = %q, want Bearer public", got)
	}

	// 3. x-opencode-client
	if got := header.Get("x-opencode-client"); got != "cli" {
		t.Errorf("x-opencode-client = %q, want cli", got)
	}

	// 4. x-opencode-project (must be 40 hex characters)
	project := header.Get("x-opencode-project")
	if !hex40Pattern.MatchString(project) {
		t.Errorf("x-opencode-project %q does not match hex40 pattern", project)
	}

	// 5. Distributed trace headers: b3 and traceparent
	b3 := header.Get("b3")
	if !b3Pattern.MatchString(b3) {
		t.Errorf("b3 %q does not match pattern <hex32>-<hex16>-1-<hex16>", b3)
	}

	tp := header.Get("traceparent")
	if !traceparentPattern.MatchString(tp) {
		t.Errorf("traceparent %q does not match 00-<hex32>-<hex16>-01", tp)
	}

	// 6. Session headers
	ses := header.Get("x-opencode-session")
	if !SessionPattern.MatchString(ses) {
		t.Errorf("x-opencode-session %q does not match %s", ses, SessionPattern)
	}
	if header.Get("x-session-affinity") != ses {
		t.Errorf("x-session-affinity %q != %q", header.Get("x-session-affinity"), ses)
	}
	if header.Get("x-opencode-session-id") != ses {
		t.Errorf("x-opencode-session-id %q != %q", header.Get("x-opencode-session-id"), ses)
	}
}

func TestZenV2FingerprintToolDefinitions(t *testing.T) {
	// Read tool
	readDef, ok := fingerprintDefinitions["read"]
	if !ok {
		t.Fatalf("missing read tool definition")
	}
	readParams, ok := readDef.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("read parameters.properties is not map[string]any")
	}

	offset, ok := readParams["offset"].(map[string]any)
	if !ok || offset["type"] != "number" {
		t.Errorf("read.offset type = %v, want number", offset["type"])
	}
	if _, hasMin := offset["minimum"]; hasMin {
		t.Errorf("read.offset should not declare minimum")
	}

	limit, ok := readParams["limit"].(map[string]any)
	if !ok || limit["type"] != "number" {
		t.Errorf("read.limit type = %v, want number", limit["type"])
	}
	if _, hasMin := limit["minimum"]; hasMin {
		t.Errorf("read.limit should not declare minimum")
	}
	if _, hasAdd := readDef.Parameters["additionalProperties"]; hasAdd {
		t.Errorf("read parameters should not have additionalProperties")
	}

	// Shell tool
	shellDef, ok := fingerprintDefinitions["shell"]
	if !ok {
		t.Fatalf("missing shell tool definition")
	}
	shellParams, ok := shellDef.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("shell parameters.properties is not map[string]any")
	}

	timeout, ok := shellParams["timeout"].(map[string]any)
	if !ok || timeout["type"] != "number" {
		t.Errorf("shell.timeout type = %v, want number", timeout["type"])
	}
	if _, hasMin := timeout["minimum"]; hasMin {
		t.Errorf("shell.timeout should not declare minimum")
	}
	if _, hasAdd := shellDef.Parameters["additionalProperties"]; hasAdd {
		t.Errorf("shell parameters should not have additionalProperties")
	}
}

func TestDefaultConfigModelIsActive(t *testing.T) {
	cfg := config.Default()
	if cfg.DefaultModel != "big-pickle" {
		t.Errorf("DefaultModel = %q, want big-pickle", cfg.DefaultModel)
	}
	// Verify no fake model aliases exist (original upstream names only)
	if len(cfg.ModelAliases) != 0 {
		t.Errorf("expected empty ModelAliases, got %v", cfg.ModelAliases)
	}
	if got := ResolveModel(cfg, "big-pickle"); got != "big-pickle" {
		t.Errorf("ResolveModel(big-pickle) = %q, want big-pickle", got)
	}
	if got := ResolveModel(cfg, "mimo-v2.6-flash-free"); got != "mimo-v2.6-flash-free" {
		t.Errorf("ResolveModel(mimo-v2.6-flash-free) = %q, want mimo-v2.6-flash-free", got)
	}
	if got := ResolveModel(cfg, "default"); got != "big-pickle" {
		t.Errorf("ResolveModel(default) = %q, want big-pickle", got)
	}
	if got := ResolveModel(cfg, ""); got != "big-pickle" {
		t.Errorf("ResolveModel empty = %q, want big-pickle", got)
	}

	// Verify static catalog advertises real models, not synthetic aliases
	models := staticModels(cfg)
	foundBigPickle := false
	for _, m := range models {
		if m.ID == "big-pickle" {
			foundBigPickle = true
		}
		if m.ID == "gpt-4o" || m.ID == "claude-3-5-sonnet" {
			t.Errorf("staticModels should not contain fake alias %q", m.ID)
		}
	}
	if !foundBigPickle {
		t.Errorf("staticModels missing big-pickle")
	}
}

