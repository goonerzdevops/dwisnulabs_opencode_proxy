# Plan: Lightweight OpenCode OpenAI-Compatible Proxy Daemon

## 1. Executive Summary & Tech Stack Selection

Tujuan dari proyek ini adalah membangun daemon proxy mandiri (standalone), ultra-ringan, minim dependensi, yang mengekspos endpoint **OpenAI-Compatible** (`/v1/chat/completions`, `/v1/models`) pada **Port 20144** (berbeda dari 9Router di port 20128) dan meneruskannya ke public free-tier endpoint **OpenCode** (`https://opencode.ai/inference/v1/...`) dengan teknik impersonation, session stabilization, dan **outbound proxy rotation** untuk mencegah rate limit IP.

---

### 1.1 Apakah Butuh TUI?
**TIDAK.** TUI (*Text User Interface* seperti Bubble Tea atau Blessed) dirancang untuk aplikasi interaktif foreground di terminal (membutuhkan TTY/PTY). 
Karena target deployment adalah **`systemd` daemon**, memasang TUI justru:
1. Tidak berfungsi / error karena systemd berjalan non-interactive tanpa terminal (headless).
2. Memboroskan resource CPU & RAM untuk rendering loop ANSI terminal.
3. Yang dibutuhkan adalah **Headless Daemon** dengan:
   - Standard stdout/stderr logging (terintegrasi otomatis dengan `journalctl -u opencode-proxy -f`).
   - Optional CLI subcommands (`opencode-proxy start`, `opencode-proxy status`, `opencode-proxy test`).
   - Optional lightweight JSON `/health` endpoint untuk health check liveness.

---

### 1.2 Apakah Perlu Ada "Agent" di Dalam Proxy atau Murni Endpoint Proxy?

**Jawabannya: MURNI ENDPOINT PROXY (Transparent Gateway / Wire Translator).**

Proxy **TIDAK PERLU** dan **TIDAK BOLEH** menjadi Agent tersendiri.
- **Siapa Agent-nya?** Agent-nya adalah **Cline**, **Claude Code**, **Hermes**, **Roo Code**, atau **Aider** yang bertindak sebagai *client*.
- **Agentic Loop:** Siklus pengambilan keputusan, eksekusi tools di OS (baca file, edit kode, eksekusi terminal bash), serta penambahan hasil tool ke riwayat percakapan (`role: "tool"`) adalah tanggung jawab penuh dari **Cline**.

#### Bagaimana Proxy Menangani Tool Calling (Agentic) vs Chatting Biasa?

Proxy bertindak sebagai **jembatan transparan** yang meniru cara 9Router menyelesaikan masalah gatekeeping upstream:

```
[ Client: Cline / WebUI ]
       │
       ▼  1. Request OpenAI-Compatible (chat biasa ATAU array `tools: [...]`)
[ OpenCode Proxy (Port 20144) ]
       │  2. Autentikasi API Key client (default: sk-opencode-free / permissive)
       │  3. Sanitize reasoning turn lama (cegah 400 Responses API)
       │  4. Injeksi header OpenCode (UA v2, canonical ses_..., Bearer public)
       │  5. Tool Merging & Collision Aliasing (Kunci Bypass Free-Tier)
       │  6. Outbound Proxy Rotator (Direct / SOCKS5 / HTTP failover on 429)
       ▼
[ Upstream: opencode.ai/inference/v1 ]
       │
       ▼  7. Upstream stream tokens / tool_calls delta chunks
[ OpenCode Proxy ]
       │  8. Realtime Passthrough SSE (termasuk streaming tool_calls)
       ▼
[ Client: Cline ]  -> Eksekusi tool lokal (baca file/bash) -> Balas ke Proxy turn berikutnya
```

| Kasus Penggunaan | Yang Dikirim Client | Apa yang Dilakukan Proxy | Hasil ke Client |
| :--- | :--- | :--- | :--- |
| **Kasus A: Chatting Biasa** (Tanpa Tool Calling, misal curl / WebUI) | `messages: [...]`<br>`tools: []` (kosong) | Proxy **menyuntikkan stub fingerprint tools** (`read`, `shell`) ke upstream agar upstream tidak memblokir 403 FreeTierError. | Jawaban teks normal streaming/non-streaming sampai ke user. Model tidak memanggil tool karena system prompt user tidak memintanya. |
| **Kasus B: Agentic AI (Cline, Claude Code, Aider)** | `messages: [...]`<br>`tools: [cline_tools...]` | 1. Proxy **mempertahankan seluruh tool milik Cline**.<br>2. Proxy **menggabungkan (merge)** fingerprint tool OpenCode.<br>3. Jika ada nama tool yang bentrok (misal Cline punya tool bernama `read`), proxy melakukan **temporary aliasing**.<br>4. Streaming `tool_calls` delta diteruskan langsung ke Cline. | Cline menerima `tool_calls`, mengeksekusi tool di mesin lokal user, lalu mengirim pesan `role: "tool"` pada giliran berikutnya secara seamless. |

---

### 1.3 Apakah Perlu Database (DB)?

**Jawabannya: TIDAK PERLU Database Eksternal (No MySQL/PostgreSQL/SQLite).**

- **Alasan**:
  1. Proxy ini dirancang **super ringan (RAM 10-20 MB)** dan zero external runtime dependency.
  2. Semua konfigurasi (port, default model, model aliases, API keys, proxy pool) disimpan dalam **Atomic JSON Config File** (`config.json`).
  3. Format JSON sangat mudah dibaca, diedit langsung pakai text editor jika perlu, dan aman ditulis secara atomik oleh Go tanpa perlu database engine.
  4. Sesi canonical (`ses_...`) dan token rate-limiting disimpan di **in-memory thread-safe cache (`sync.Map`)** dengan TTL 30-60 menit (hanya memakan < 2 MB RAM).

---

### 1.4 Embedded Web UI Dashboard (Untuk Ganti Default Model ID, Test Model, & Monitoring)

Karena Anda membutuhkan UI untuk **memilih/mengganti default model ID dan menguji model**, solusinya adalah **Embedded Single-Page Web Dashboard** di dalam binary Go:

- **Teknologi**: Menggunakan fitur bawaan Go **`//go:embed`** untuk menyatukan HTML, CSS, dan JavaScript (Vanilla JS, Dark Modern Aesthetics) langsung ke dalam satu binary executable.
  - **Zero file eksternal**: Tidak perlu install Node.js, tidak perlu web server terpisah (Nginx/Apache).
  - Akses langsung di browser: `http://127.0.0.1:20144/` (atau IP Home Server Anda).
- **Fitur Web UI**:
  1. **Default Model Selector**: Dropdown / kartu pilihan model aktif (misal `muse-spark-1.3-contributor-free`, `jev-1.13-free`).
  2. **Model Tester / Playground Sederhana**: Tombol *"Test Model"* untuk mengecek apakah model upstream sedang online/hidup dan responsif.
  3. **Model Aliases Mapping**: Memetakan alias model populer (misal jika Cline meminta `gpt-4o` atau `claude-3-5-sonnet`, otomatis di-redirect ke model OpenCode yang dipilih).
  4. **API Key Settings**: Menampilkan/mengganti API Key, atau toggle **Permissive Mode** (terima sembarang key).
  5. **Proxy Pool Status**: Menampilkan status IP Direct & Outbound Proxies (apakah sedang sehat atau sedang cooldown).
  6. **Hot-Reload**: Tombol *"Save Settings"* langsung menulis ke `config.json` dan mengupdate memori daemon tanpa perlu merestart systemd service!

---

### 1.5 Dukungan API Key (OpenAI Compatibility Standard)

Klien OpenAI (seperti Cline, Continue.dev, Cursor, Aider, atau Python SDK `openai.OpenAI()`) **selalu mewajibkan** adanya API Key di header `Authorization: Bearer <API_KEY>`.

#### Solusi Implementasi API Key di Proxy:
1. **Default API Key**:
   - Out-of-the-box default key: **`sk-opencode-free`** (dapat diubah via Web UI atau `config.json`).
2. **Permissive Mode (Opsional & Sangat Praktis)**:
   - Jika opsi `permissive_auth: true` diaktifkan (default `true`), proxy akan menerima API key apa pun yang dikirim client (misal dummy key `sk-123456`, `test`, atau `any`), sehingga Anda tidak perlu repot menyamakan key di setiap extension IDE.
3. **Upstream Key Swapping**:
   - Berapa pun API key yang dikirim oleh Cline, sebelum request dilempar ke server OpenCode upstream, proxy akan **menggantinya secara otomatis** menjadi:
     ```http
     Authorization: Bearer public
     ```
     sehingga request lolos sebagai free-tier publik OpenCode.

---

### 1.6 Analisis Efek Deployment: Pasang di WSL vs Pasang di Home Server

Berikut analisis komparatif efek ketika proxy dipasang di WSL (laptop/PC lokal) versus di Home Server (Dedicated mini PC / server rumah):

| Aspek | Pasang di WSL (Local PC) | Pasang di Home Server (Server LAN / Always-On) |
| :--- | :--- | :--- |
| **Ketersediaan (Uptime)** | Hanya menyala saat PC Windows hidup. Jika PC sleep / restart, WSL mati dan proxy terputus. | **24/7 Always-On**. Berjalan stabil di background tanpa terpengaruh kondisi PC kerja Anda. |
| **Aksesibilitas Multi-Device** | Umumnya hanya diakses dari PC itu sendiri via `http://localhost:20144/v1`. | **Bisa diakses seluruh perangkat di rumah**: PC desktop, laptop, HP, atau tablet cukup mengarahkan `baseURL` ke `http://192.168.x.x:20144/v1`. |
| **IP Publik WAN (Sumber Masalah Rate-Limit)** | Berbagi IP publik internet rumah yang sama. | Berbagi IP publik internet rumah yang sama. |
| **Dampak Rate-Limit (429)** | Jika Cline dipakai intensif, IP rumah terkena 429 oleh Cloudflare. | Jika Cline dipakai intensif, seluruh perangkat di rumah terkena 429 oleh Cloudflare. |
| **Peran Proxy Rotation** | **PENTING**: Mencegah IP rumah di-block saat coding marathon. | **SANGAT KRUSIAL**: Melindungi server agar tidak macet total dan dapat melayani banyak client tanpa gangguan. |

---

### 1.7 Arsitektur Proxy Rotation (Outbound Pool & Failover)

Untuk meniru keunggulan sistem rotation (seperti pada 9Router dan `opencode2api`) namun tetap menjaga kode **super ringan**:

#### Mengapa Perlu Proxy Rotation?
1. **Bypass IP Quota di Free-Tier**: Kuota `Bearer public` OpenCode dihitung per IP address.
2. **Menghindari Cloudflare WAF Cooldown**: Saat Cline menembakkan puluhan tool call dalam waktu singkat, Cloudflare bisa memicu temporary block (15-60 menit).
3. **Fleksibilitas Koneksi**: Memungkinkan proxy menggunakan IP lokal Anda secara default, tetapi otomatis beralih (*failover*) ke proxy lain jika terjadi error.

#### Cara Kerja Proxy Rotation di Proyek Kita:
Proxy mendukung konfigurasi pool di `config.json`:
```json
"proxies": [
  "direct",
  "socks5://127.0.0.1:7890",
  "http://127.0.0.1:10808"
]
```

1. **Strategi `failover_on_error` (Default & Paling Efisien)**:
   - Proxy memprioritaskan koneksi langsung (`direct`) tanpa overhead.
   - Jika upstream mengembalikan status **`429 Too Many Requests`** atau **`403 Forbidden`**:
     - IP yang bermasalah dimasukkan ke status *cooldown* selama 15 menit.
     - Request secara transparan **diulang (replay) seketika** menggunakan proxy berikutnya di dalam pool (misal SOCKS5 Clash/v2ray lokal).
     - Klien (Cline) tidak merasakan error sama sekali dan pekerjaan terus berjalan lancar.
2. **Strategi `round_robin` (Opsional)**:
   - Setiap sesi client baru diberikan jalur proxy bergantian dari daftar pool untuk mendistribusikan beban secara merata.

---

### 1.8 Rekomendasi Bahasa & Tech Stack: **Go (Golang)**

| Kriteria | **Go (Rekomendasi Utama)** | Rust | Node.js / Bun | Python |
| :--- | :--- | :--- | :--- | :--- |
| **Footprint RAM** | **~10 - 20 MB** (Termasuk Web UI, Cache & Proxy Pool) | ~5 - 10 MB | ~50 - 100 MB | ~40 - 70 MB |
| **Startup Time** | **< 20 ms** | < 10 ms | ~80 - 150 ms | ~200 - 400 ms |
| **Distribusi Binary** | **Single static binary** (`CGO_ENABLED=0`) | Single static binary | Perlu runtime / binary bundler besar (>50MB) | Perlu runtime Python + virtualenv |
| **Zero External Deps** | **Bisa 100% Go Standard Library** (`net/http`, `embed`) | Perlu tokio, axum, serde | Perlu npm packages | Perlu uvicorn, httpx, fastapi |
| **SSE Streaming** | Native dengan `http.Flusher` & `bufio.Reader` | Cukup kompleks (async streams) | Mudah | Mudah tapi CPU overhead |
| **Systemd Integration** | Sangat natural (`os/signal`, exit codes) | Sangat natural | Perlu wrapper process | Perlu wrapper process |

---

## 2. Reverse-Engineering Specs (Berdasarkan 9Router & OpenCode)

### 2.1 Port & Upstream Endpoints
- **Proxy Port Default**: **`20144`** (dapat diubah via flag `--port` atau env `OPENCODE_PROXY_PORT`).
- **Base URL Upstream**: `https://opencode.ai`
- **Routing Jalur (Update Oktober 2026)**:
  - Model Responses API (misal `muse-spark-1.2-contributor-free`, `muse-spark-1.3`):
    `POST https://opencode.ai/inference/v1/responses`
  - Model Chat Standar (misal `jev-1.13-free` / union-alpha / dll):
    `POST https://opencode.ai/inference/v1/chat/completions`
  - Model List:
    `GET https://opencode.ai/zen/v1/models` (atau static catalog fallback)

### 2.2 Wajib Impersonation Headers
```http
Authorization: Bearer public
User-Agent: opencode/latest/2.0.26/cli
x-opencode-client: cli
x-opencode-session: ses_<12_hex_timestamp_counter><14_char_base62>
x-opencode-session-id: <ses_canonical>
x-session-affinity: <ses_canonical>
x-session-id: <ses_canonical>
x-opencode-project: global
Content-Type: application/json
```

### 2.3 Session ID Canonical Generator & LRU Cache
Format session ID OpenCode:
`ses_` + `12 karakter hex` + `14 karakter random base62`
- 12 hex merupakan representasi `(timestampMs * 4096) + counter`.
- **Session Cache**: Proxy menyimpan session ID stabil per client IP / Auth header di memory selama TTL (misal 30-60 menit) agar tidak memicu 429 session abuse.

### 2.4 Fingerprint Tool Injection & Collision Handler
Upstream OpenCode free tier memvalidasi deklarasi tool bawaan CLI OpenCode (`read`, `shell`).
- **Kasus Chat biasa**: Proxy menyuntikkan tool stub fingerprint ini.
- **Kasus Agentic (Cline)**: Proxy menyisipkan fingerprint tool ke dalam array tools client. Jika nama tool client bentrok (`read`/`shell`), proxy me-rename sementara dan me-restore namanya pada saat response stream parsing.

### 2.5 Request & Response Normalization
1. **Always Stream Upstream**:
   Upstream menolak request non-streaming (`stream: false` -> `403 FreeTierError`). Proxy selalu kirim `stream: true`, dan mengagregasi SSE ke JSON utuh jika client downstream meminta non-stream.
2. **Sanitize Multi-turn Reasoning**:
   Membuang item reasoning lama dan menyetel `store: false` untuk mencegah error 400 pada Responses API.

---

## 3. Arsitektur Proyek (Go Lightweight)

```
opencode-proxy/
├── cmd/
│   └── opencode-proxy/
│       └── main.go              # Entrypoint CLI, web server & daemon runner
├── internal/
│   ├── config/
│   │   ├── config.go            # Struct konfigurasi + atomic JSON reader/writer
│   │   └── default.go           # Default settings (port 20144, sk-opencode-free)
│   ├── server/
│   │   ├── router.go            # HTTP multiplexer Go 1.22+
│   │   ├── handlers_openai.go   # /v1/chat/completions, /v1/models
│   │   ├── handlers_api.go      # /api/config, /api/models, /api/test, /health
│   │   └── middleware.go        # API Key validator & permissive mode check
│   ├── proxy/
│   │   ├── client.go            # Upstream HTTP client connection pooling
│   │   ├── transport.go         # Outbound Proxy Pool (Direct / SOCKS5 / HTTP failover)
│   │   ├── session.go           # Canonical session generator + in-memory cache
│   │   ├── fingerprint.go       # Tool fingerprint stub injection & collision handler
│   │   ├── transform.go         # OpenAI payload -> OpenCode payload adapter
│   │   └── streamer.go          # SSE realtime pipe & SSE-to-JSON aggregator
│   └── web/
│       ├── embed.go             # //go:embed static files ke dalam binary
│       └── static/
│           ├── index.html       # Web UI Dashboard (Dark Mode, Responsive, Model Tester)
│           ├── app.js           # Fetch models, switch default model, test prompt, update config
│           └── style.css        # Vanilla CSS modern aesthetics
├── systemd/
│   └── opencode-proxy.service   # Template systemd unit file (port 20144)
├── scripts/
│   └── install-service.sh       # Script install otomatis ke /etc/systemd/system
├── Makefile                     # Build & test helpers
└── README.md
```

---

## 4. Rincian Konfigurasi JSON (`config.json`)

Disimpan di `~/.config/opencode-proxy/config.json` (atau `/etc/opencode-proxy/config.json`):

```json
{
  "port": 20144,
  "host": "0.0.0.0",
  "api_keys": [
    "sk-opencode-free"
  ],
  "permissive_auth": true,
  "default_model": "muse-spark-1.3-contributor-free",
  "model_aliases": {
    "gpt-4o": "muse-spark-1.3-contributor-free",
    "gpt-4o-mini": "muse-spark-1.3-contributor-free",
    "claude-3-5-sonnet": "muse-spark-1.3-contributor-free",
    "claude-3-7-sonnet": "muse-spark-1.3-contributor-free"
  },
  "upstream_base_url": "https://opencode.ai",
  "session_ttl_minutes": 60,
  "proxies": [
    "direct",
    "socks5://127.0.0.1:7890",
    "http://127.0.0.1:10808"
  ],
  "proxy_rotation_strategy": "failover_on_error",
  "proxy_cooldown_seconds": 900
}
```

---

## 5. Systemd Daemon Deployment

### 5.1 Unit File (`/etc/systemd/system/opencode-proxy.service`)
```ini
[Unit]
Description=OpenCode Ultra-Lightweight OpenAI-Compatible Proxy Daemon
After=network.target

[Service]
Type=simple
User=nobody
Group=nogroup
ExecStart=/usr/local/bin/opencode-proxy --config /etc/opencode-proxy/config.json
Restart=always
RestartSec=3
LimitNOFILE=65535

# Security hardening
ProtectSystem=full
ProtectHome=true
NoNewPrivileges=true
PrivateTmp=true

# Logs ke journalctl
StandardOutput=journal
StandardError=journal
SyslogIdentifier=opencode-proxy

[Install]
WantedBy=multi-user.target
```

### 5.2 Perintah Manajemen Daemon
- Reload systemd: `sudo systemctl daemon-reload`
- Aktifkan autostart: `sudo systemctl enable opencode-proxy`
- Jalankan service: `sudo systemctl start opencode-proxy`
- Cek log streaming: `journalctl -u opencode-proxy -f`
- Cek status: `systemctl status opencode-proxy`

---

## 6. Phased Implementation Roadmap

### Phase 1: Core Foundation & Config Manager (Hari 1)
- Setup Go module & atomic JSON config reader/writer (termasuk field `proxies` dan `proxy_rotation_strategy`).
- Buat HTTP Server dengan Go standard library (`net/http`) listening di `0.0.0.0:20144` agar bisa diakses LAN/WSL.
- Implementasi API Key validation middleware + permissive auth support.
- Implementasi session generator canonical OpenCode v2 (`ses_<12hex><14base62>`).

### Phase 2: Upstream Adapter, Headers & Tool Fingerprinting (Hari 2)
- Implementasi logic `Authorization: Bearer public` dan headers impersonation v2.
- Implementasi injector & merger fingerprint tools (`read`, `shell`) yang aman terhadap tools milik Cline.
- Implementasi transformer payload (OpenAI format -> OpenCode Inference format).
- Model alias resolution (misal request `gpt-4o` di-remap ke `default_model`).
- Strip reasoning items dari turn sebelumnya.

### Phase 3: Outbound Proxy Pool & Failover Engine (Hari 3)
- Implementasi `internal/proxy/transport.go`:
  - Support `direct`, SOCKS5 proxy, dan HTTP proxy via standard dialer.
  - Tracking status cooldown per proxy node (jika kena 429 atau 403, cooldown 15 menit).
  - Mekanisme replay transparan saat request pertama terkena 429.

### Phase 4: Realtime Tool Calling SSE Streaming & Aggregation (Hari 4)
- Realtime SSE forwarder dengan `http.Flusher` (support teks stream + tool_calls delta chunks).
- Buffer aggregator untuk downstream non-streaming requests.

### Phase 5: Embedded Web UI Dashboard & Model Tester (Hari 5)
- Buat file frontend static (`index.html`, `style.css`, `app.js`) dengan dark mode modern.
- Embed static files ke binary menggunakan `//go:embed`.
- Fitur UI:
  - Ganti Default Model.
  - Tombol *"Test Model"* (kirim prompt uji coba cepat ke upstream).
  - Status proxy pool (Direct / SOCKS5 node health).
  - REST API internal: `GET /api/config`, `POST /api/config`, `POST /api/test`, `GET /api/models`.

### Phase 6: Systemd Packaging & Client Testing (Hari 6)
- Makefile untuk cross-compile Linux AMD64/ARM64 (`CGO_ENABLED=0`).
- Script installer systemd service.
- Uji integrasi live dengan:
  - **Web UI di browser**: `http://127.0.0.1:20144/` (atau IP Home Server).
  - **Cline**: Menggunakan `baseURL: http://<ip>:20144/v1` dan `apiKey: sk-opencode-free`.
  - **Test Failover**: Simulasi trigger rate-limit untuk membuktikan proxy otomatis berpindah ke node berikutnya.