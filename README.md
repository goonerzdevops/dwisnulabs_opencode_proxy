# OpenCode Proxy (`dwisnulabs_opencode_proxy`)

High-performance, zero-config local proxy for OpenCode free coding models with anti-abuse protection, session affinity, token telemetry, and full Agentic AI support (Cline, Roo Code, Cursor, Aider, Claude Code).

---

## ✨ Features

- **OpenAI Compatible**: Seamlessly exposes `/v1/chat/completions` and `/v1/models` so any coding tool or AI harness works immediately.
- **Agentic AI First-Class Support**: Designed specifically for multi-turn agentic coding workflows (tested extensively with Cline, Roo Code, and Claude Code).
- **Anti-Abuse & Rate-Limit Protection**: Built-in pacing, concurrency limiter, and transparent proxy failover with automatic node cooldowns.
- **OpenCode Gate Protocol**: Strictly compliant with upstream OpenCode CLI v2.1.3 gate requirements:
  - Verbatim fingerprint tools (`read` & `shell`) with seamless client tool renaming and restoration (`hidden_read_1` $\rightarrow$ `read`).
  - Pre-emptive `<env>` marker injection that neutralizes client-side collisions.
  - Authentic session IDs (`ses_<12hex><14base62>`), 40-character project IDs, and distributed tracing headers (`b3`, `traceparent`).
- **Real-Time Web Dashboard**: Integrated local dashboard at `http://127.0.0.1:20144/` with:
  - Live model testing playground (SSE streaming, token metrics, latency).
  - Quick model presets and active upstream catalog with instant refresh.
  - Real-time token telemetry (cache hit rate, prompt/output tokens, request history).
  - Web-based configuration management (`config.json`) with hot-reload.

---

## 🚀 Quick Start

### 1. Requirements
- **Go 1.22+** (or use pre-built binary for Windows/Linux)
- Git

### 2. Build from Source
```bash
# Clone the repository
git clone https://github.com/goonerzdevops/dwisnulabs_opencode_proxy.git
cd dwisnulabs_opencode_proxy

# Build binary
go build -o bin/opencode-proxy.exe ./cmd/opencode-proxy
```

### 3. Run the Proxy
```powershell
# Run directly
./bin/opencode-proxy.exe

# Or run as a daemon on Windows
./daemon.ps1 start
```

Default listening port is `20144`.
- **Web Dashboard**: `http://127.0.0.1:20144/`
- **OpenAI Endpoint**: `http://127.0.0.1:20144/v1`

---

## 🛠️ Configuration (`config.json`)

```json
{
  "port": 20144,
  "host": "127.0.0.1",
  "api_keys": [
    "sk-opencode-free"
  ],
  "permissive_auth": true,
  "default_model": "big-pickle",
  "model_aliases": {},
  "upstream_base_url": "https://opencode.ai",
  "session_ttl_minutes": 60,
  "upstream_timeout_seconds": 600,
  "log_requests": true,
  "follow_upstream_models": true,
  "system_prompt_tuning": "",
  "proxies": [
    "direct"
  ],
  "proxy_cooldown_seconds": 60,
  "anti_abuse_enabled": true,
  "max_concurrent_per_node": 1,
  "min_request_interval_ms": 800
}
```

---

## 🤖 Client Setup

### Cline / Roo Code (VS Code Extension)
1. Open Cline Settings $\rightarrow$ **API Provider**: `OpenAI Compatible`
2. **Base URL**: `http://127.0.0.1:20144/v1`
3. **API Key**: `sk-opencode-free`
4. **Model ID**: `big-pickle` (or `glm-5.3-flash-free`, `mimo-v2.6-flash-free`, `nemotron-3.5-lightning-free`)

### Cursor
1. Go to **Settings** $\rightarrow$ **Models** $\rightarrow$ **OpenAI API Key**
2. Base URL: `http://127.0.0.1:20144/v1`
3. API Key: `sk-opencode-free`
4. Add Model Name: `big-pickle`

### Aider
```bash
aider --openai-api-base http://127.0.0.1:20144/v1 --openai-api-key sk-opencode-free --model openai/big-pickle
```

---

## 🧠 Supported Active Free Models

Upstream OpenCode provides active free models that are dynamically refreshed:
- `big-pickle` *(Recommended default - powerful general coding & reasoning)*
- `glm-5.3-flash-free` *(Fast coding & analysis)*
- `mimo-v2.6-flash-free` *(Fast lightweight completions)*
- `nemotron-3.5-lightning-free` *(Fast response generation)*

---

## 🧪 Testing

Run comprehensive agentic integration tests:
```bash
# Run unit & agentic integration tests
go test -v ./internal/proxy -run "TestAgentic"

# Run all test suites
go test -v ./internal/...
```

---

## 📄 License

MIT License. Free to use for personal and development workflows.
