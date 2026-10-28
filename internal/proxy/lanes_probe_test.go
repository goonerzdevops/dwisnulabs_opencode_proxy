package proxy

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"opencode-proxy/internal/config"
)

// TestLaneProbe is an opt-in diagnostic that exercises each free-tier model
// through the production request path (Prepare -> Upstream.Send) and maps the
// other upstream lanes. It hits the live upstream, so it is skipped unless
// OPENCODE_LANE_PROBE=1.
//
//	$env:OPENCODE_LANE_PROBE="1"; go test ./internal/proxy -run TestLaneProbe -v
func TestLaneProbe(t *testing.T) {
	if os.Getenv("OPENCODE_LANE_PROBE") != "1" {
		t.Skip("set OPENCODE_LANE_PROBE=1 to probe the live free tier")
	}

	cfg := config.Default()
	up := NewUpstream(cfg, nil)
	fp := Fingerprint{
		SessionID: TranslateSessionID("lane-probe", "generic"),
		Project:   UpstreamProject,
	}

	models := strings.Split(os.Getenv("OPENCODE_LANE_PROBE_MODELS"), ",")
	if len(models) == 1 && strings.TrimSpace(models[0]) == "" {
		models = defaultProbeModels()
	}

	// Pass 1: production path per model (Prepare injects the fingerprint tools).
	for _, model := range models {
		model = strings.TrimSpace(model)
		if model == "" {
			continue
		}
		prepared, err := Prepare(cfg, probeChatBody(model))
		if err != nil {
			fmt.Printf("%-34s %-10s PREPARE-ERR  %v\n", model, "prod", err)
			continue
		}
		status, summary := probeSend(up, cfg, prepared, fp)
		fmt.Printf("%-34s %-10s %d  %s\n", model, "prod", status, summary)
	}

	// Pass 2: path matrix on a known-good chat model, per-lane bodies with the
	// fingerprint applied the way each lane's wire format needs it.
	probeModel := os.Getenv("OPENCODE_LANE_PROBE_PATH_MODEL")
	if probeModel == "" {
		probeModel = "deepseek-v4.1-flash-free"
	}
	for _, lane := range []Lane{LaneChat, LaneResponses, LaneMessages} {
		prepared, err := Prepare(cfg, probeChatBody(probeModel))
		if err != nil {
			t.Fatalf("prepare %s: %v", probeModel, err)
		}
		if lane == LaneChat {
			continue // covered by pass 1
		}
		prepared.Lane = lane
		prepared.Path = UpstreamPath(lane)
		prepared.Body = probeLaneBody(lane, probeModel)
		if lane == LaneResponses {
			ApplyFingerprintTools(prepared.Body, true)
		} else {
			ApplyFingerprintTools(prepared.Body, false)
		}
		status, summary := probeSend(up, cfg, prepared, fp)
		fmt.Printf("%-34s %-10s %d  %s\n", probeModel, string(lane), status, summary)
	}
}

func defaultProbeModels() []string {
	return []string{
		"beam-free",
		"deepseek-v4.1-flash-free",
		"glm-5.3-flash-free",
		"jev-1.13-free",
		"longcat-2.5-preview-free",
		"mimo-v2.6-flash-free",
		"nemotron-3.5-lightning-free",
		"nemotron-3.5-super-vl-free",
		"space-bunny-free",
		"muse-spark-1.2",
		"muse-spark-1.3",
		"muse-spark-1.2-contributor-free",
		"union-alpha",
	}
}

func probeSend(up *Upstream, cfg config.Config, prepared *PreparedRequest, fp Fingerprint) (int, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	response, err := up.Send(ctx, cfg, prepared, fp)
	if err != nil {
		return 0, "ERR  " + err.Error()
	}
	defer response.Body.Close()
	blob, _ := io.ReadAll(io.LimitReader(response.Body, 2048))
	summary := strings.Join(strings.Fields(string(blob)), " ")
	if len(summary) > 160 {
		summary = summary[:160]
	}
	return response.StatusCode, summary
}

// probeChatBody is a minimal production-shaped Chat Completions request.
func probeChatBody(model string) map[string]any {
	return map[string]any{
		"model":      model,
		"stream":     true,
		"max_tokens": 16,
		"messages":   []any{map[string]any{"role": "user", "content": "Say pong"}},
	}
}

// probeLaneBody builds a hand-written body for a non-production lane probe.
func probeLaneBody(lane Lane, model string) map[string]any {
	if lane == LaneResponses {
		return map[string]any{
			"model":             model,
			"stream":            true,
			"store":             false,
			"max_output_tokens": 16,
			"input": []any{map[string]any{
				"type":    "message",
				"role":    "user",
				"content": []any{map[string]any{"type": "input_text", "text": "Say pong"}},
			}},
		}
	}
	return map[string]any{
		"model":      model,
		"stream":     true,
		"max_tokens": 16,
		"messages":   []any{map[string]any{"role": "user", "content": "Say pong"}},
	}
}

// TestFingerprintHeadersDocumentsContract pins the header set the free-tier gate
// validates so a refactor cannot silently drop one (that yields 403 FreeTierError).
func TestFingerprintHeadersDocumentsContract(t *testing.T) {
	up := NewUpstream(config.Default(), nil)
	header := http.Header{}
	up.applyHeaders(header, true, Fingerprint{SessionID: TranslateSessionID("hdr", "generic")})

	for _, name := range []string{
		"Authorization", "User-Agent", "x-opencode-client", "x-opencode-project",
		"x-opencode-session", "x-opencode-session-id", "x-session-affinity", "x-session-id",
		"b3", "traceparent",
	} {
		if header.Get(name) == "" {
			t.Fatalf("header %s missing from fingerprint set", name)
		}
	}
	if got := header.Get("Accept"); got != "text/event-stream" {
		t.Fatalf("Accept = %q, want text/event-stream", got)
	}
	if !SessionPattern.MatchString(header.Get("x-opencode-session")) {
		t.Fatalf("session %q does not match %s", header.Get("x-opencode-session"), SessionPattern)
	}
	if len(header.Get("x-opencode-project")) != 40 {
		t.Fatalf("x-opencode-project %q must be 40 hex chars", header.Get("x-opencode-project"))
	}
}
