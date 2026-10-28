# OpenCode Proxy — API & Endpoint Documentation

Dokumentasi lengkap seluruh endpoint yang tersedia pada **OpenCode Proxy Daemon** (Port `20144`).

---

## 1. Ikhtisar & Informasi Gateway

- **Base URL**: `http://127.0.0.1:20144`
- **OpenAI Compatible Base URL**: `http://127.0.0.1:20144/v1`
- **Default Port**: `20144`
- **Autentikasi**: Header `Authorization: Bearer <api_key>`
- **Permissive Auth**: Secara default aktif (`"permissive_auth": true`). Klien dapat mengirimkan API key apa saja (misal `sk-opencode-free`).

---

## 2. Tabel Ringkasan Endpoint

| Method | Endpoint Path | Autentikasi | Deskripsi / Fungsi |
| :--- | :--- | :---: | :--- |
| **`POST`** | `/v1/chat/completions` | Ya (Bearer) | **Endpoint Utama**: OpenAI Chat Completions (Streaming & Non-Streaming). |
| **`GET`** | `/v1/models` | Ya (Bearer) | Mengambil daftar model yang tersedia (live upstream + alias). |
| **`POST`** | `/v1/messages` | Ya (Bearer) | Guard endpoint untuk klien Anthropic (mengarahkan ke `/v1/chat/completions`). |
| **`GET`** | `/api/config` | Ya (Bearer) | Membaca konfigurasi aktif (`config.json`) dan path filenya di disk. |
| **`PUT`** | `/api/config` | Ya (Bearer) | Memperbarui konfigurasi secara *hot-reload* tanpa perlu restart daemon. |
| **`GET`** | `/api/telemetry` | Ya (Bearer) | Metrik token real-time, persentase cache hit, dan riwayat 50 request terakhir. |
| **`POST`** / **`DELETE`** | `/api/telemetry/clear` / `/api/telemetry` | Ya (Bearer) | Mereset seluruh metrik telemetri di memori (RAM) dan menghapus log WAL. |
| **`GET`** | `/health` / `/healthz` | Tidak (Publik) | Status kesehatan daemon, uptime, hitungan request, dan status proxy pool. |
| **`GET`** | `/` | Tidak (Publik) | **Web Dashboard**: GUI terintegrasi (Playground SSE, Telemetry Table, Model Switcher). |

---

## 3. Detail Setiap Endpoint

### 3.1. `POST /v1/chat/completions`
Endpoint inti yang sepenuhnya kompatibel dengan standar OpenAI API. Mendukung tool calling, streaming chunk real-time (SSE), token buffer aggregator, serta auto-injeksi Master Prompt Tuning.

#### Request Headers
```http
Content-Type: application/json
Authorization: Bearer sk-opencode-free
```

#### Request Body
```json
{
  "model": "deepseek-v4.1-flash-free",
  "messages": [
    {
      "role": "system",
      "content": "You are a helpful coding assistant."
    },
    {
      "role": "user",
      "content": "Jelaskan konsep Dependency Injection dalam 2 kalimat."
    }
  ],
  "stream": true,
  "temperature": 0.7
}
```

> [!TIP]
> **Model Alias Didukung**: Anda bisa menggunakan model ID asli (`deepseek-v4.1-flash-free`, `beam-free`) maupun alias (`default`, `gpt-4o`, `gpt-4o-mini`, `claude-3-5-sonnet`). Gateway otomatis me-routing alias ke `default_model` yang aktif.

#### Contoh Response (Non-Streaming: `stream = false`)
```json
{
  "id": "021792249400466a2f4933af3443b601e52f5659e0d958fcabe68",
  "object": "chat.completion",
  "created": 1792249400,
  "model": "deepseek-v4.1-flash-free",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Dependency Injection adalah pola desain di mana suatu objek menerima dependensi dari pihak luar alih-alih membuatnya sendiri di dalam kelas. Pola ini mempermudah pengujian kode (mocking) dan meningkatkan modularitas serta fleksibilitas aplikasi.",
        "reasoning_content": "..."
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 820,
    "completion_tokens": 64,
    "total_tokens": 884
  }
}
```

#### Contoh Response (Streaming: `stream = true`)
Respon dialirkan dalam format Server-Sent Events (SSE) dengan `flusher.Flush()` instan:
```text
data: {"id":"chatcmpl-...","object":"chat.completion.chunk","choices":[{"delta":{"content":"Dependency"}}]}

data: {"id":"chatcmpl-...","object":"chat.completion.chunk","choices":[{"delta":{"content":" Injection"}}]}

...

data: [DONE]
```

---

### 3.2. `GET /v1/models`
Mengembalikan katalog model yang didukung, baik model gratis live dari upstream OpenCode maupun model alias yang terdaftar di konfigurasi.

#### Contoh Request (cURL)
```bash
curl -s http://127.0.0.1:20144/v1/models \
  -H "Authorization: Bearer sk-opencode-free"
```

#### Contoh Response
```json
{
  "object": "list",
  "data": [
    {
      "id": "deepseek-v4.1-flash-free",
      "object": "model",
      "owned_by": "opencode"
    },
    {
      "id": "beam-free",
      "object": "model",
      "owned_by": "opencode"
    },
    {
      "id": "mimo-v2.6-flash-free",
      "object": "model",
      "owned_by": "opencode"
    },
    {
      "id": "nemotron-3.5-lightning-free",
      "object": "model",
      "owned_by": "opencode"
    },
    {
      "id": "space-bunny-free",
      "object": "model",
      "owned_by": "opencode"
    },
    {
      "id": "default",
      "object": "model",
      "owned_by": "alias"
    },
    {
      "id": "gpt-4o",
      "object": "model",
      "owned_by": "alias"
    }
  ]
}
```

---

### 3.3. `GET /health` & `GET /healthz`
Endpoint publik ringan untuk monitoring daemon, load-balancer health checks, status proxy rotation pool, dan metrik penggunaan.

#### Contoh Request
```bash
curl -s http://127.0.0.1:20144/health
```

#### Contoh Response
```json
{
  "status": "ok",
  "uptime_seconds": 3600,
  "requests": 142,
  "sessions": 8,
  "upstream": "https://opencode.ai",
  "default_model": "deepseek-v4.1-flash-free",
  "proxies": [
    {
      "url": "direct",
      "healthy": true
    },
    {
      "url": "socks5://127.0.0.1:1080",
      "healthy": false,
      "cooldown_seconds_left": 420
    }
  ]
}
```

| Field | Arti |
| :--- | :--- |
| `status` | Kondisi server (`ok`). |
| `uptime_seconds` | Total detik daemon telah berjalan. |
| `requests` | Total request yang telah dilayani gateway. |
| `sessions` | Jumlah sesi klien aktif dalam cache LRU Session Store. |
| `proxies` | Daftar node proxy egress beserta status kesehatan (`healthy`) dan sisa waktu cooldown (jika terkena HTTP 429). |

---

### 3.4. `GET /api/config`
Mengambil konfigurasi yang saat ini sedang aktif di memori beserta lokasi file fisik `config.json` di komputer.

#### Contoh Request
```bash
curl -s http://127.0.0.1:20144/api/config \
  -H "Authorization: Bearer sk-opencode-free"
```

#### Contoh Response
```json
{
  "config": {
    "port": 20144,
    "host": "127.0.0.1",
    "api_keys": [
      "sk-opencode-free"
    ],
    "permissive_auth": true,
    "default_model": "deepseek-v4.1-flash-free",
    "model_aliases": {
      "default": "deepseek-v4.1-flash-free",
      "gpt-4o": "deepseek-v4.1-flash-free",
      "claude-3-5-sonnet": "deepseek-v4.1-flash-free"
    },
    "upstream_base_url": "https://opencode.ai",
    "session_ttl_minutes": 60,
    "upstream_timeout_seconds": 600,
    "log_requests": true,
    "follow_upstream_models": true,
    "system_prompt_tuning": "Berikan jawaban dengan artikulasi yang jelas, lengkap, dan tuntas dalam bahasa apa pun yang digunakan oleh pengguna...",
    "proxies": [
      "direct"
    ],
    "proxy_cooldown_seconds": 900
  },
  "path": "C:\\D\\Project\\Mix\\opencode_proxy\\config.json",
  "read_only": false
}
```

---

### 3.5. `PUT /api/config`
Memperbarui konfigurasi secara dinamis (*Hot-Reload*). Pengaturan baru langsung disimpan ke file `config.json` lokal dan disinkronkan ke memori/proxy pool tanpa memutus koneksi atau me-restart binary proxy.

#### Contoh Request
```bash
curl -X PUT http://127.0.0.1:20144/api/config \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer sk-opencode-free" \
  -d '{
    "default_model": "beam-free",
    "proxies": ["direct", "socks5://127.0.0.1:1080"],
    "system_prompt_tuning": "Gunakan Bahasa Indonesia yang baku dan artikulatif."
  }'
```

#### Response
```json
{
  "status": "updated",
  "config": { ... }
}
```

---

### 3.6. `GET /api/telemetry`
Mengembalikan metrik token real-time, persentase efisiensi cache hit dari KV/prompt cache, serta riwayat 50 request terakhir dari ring buffer in-memory proxy.

#### Contoh Request
```bash
curl -s http://127.0.0.1:20144/api/telemetry \
  -H "Authorization: Bearer sk-opencode-free"
```

#### Contoh Response
```json
{
  "total_requests": 48,
  "total_prompt_tokens": 128450,
  "total_cached_tokens": 92100,
  "total_output_tokens": 14200,
  "overall_cache_hit_pct": 71.7,
  "recent_records": [
    {
      "id": "req-1792415000",
      "timestamp": "2026-10-19T15:20:00Z",
      "requested_model": "gpt-4o",
      "actual_model": "deepseek-v4.1-flash-free",
      "lane": "chat",
      "stream": true,
      "prompt_tokens": 4200,
      "cached_tokens": 3600,
      "output_tokens": 280,
      "total_tokens": 4480,
      "cache_hit_rate": 85.7,
      "duration_ms": 1420,
      "status_code": 200
    }
  ]
}
```

| Field Telemetry | Penjelasan |
| :--- | :--- |
| `total_prompt_tokens` | Akumulasi total token input yang dikirim klien. |
| `total_cached_tokens` | Jumlah token input yang berhasil di-*hit* dari cache upstream. |
| `overall_cache_hit_pct` | Rata-rata persentase penghematan / efisiensi cache hit (`cached / prompt * 100`). |
| `total_output_tokens` | Total token jawaban/kode yang dihasilkan LLM. |
| `recent_records` | Riwayat 50 request terakhir lengkap dengan latensi, model mapping, dan per-request cache rate. |

---

### 3.7. `POST /api/telemetry/clear` / `DELETE /api/telemetry`
Mereset seluruh metrik telemetri yang tersimpan di memori RAM (ring buffer) serta menghapus file log WAL (`usage.jsonl`) di disk.

#### Contoh Request
```bash
curl -X POST http://127.0.0.1:20144/api/telemetry/clear \
  -H "Authorization: Bearer sk-opencode-free"
```
*(Atau menggunakan `DELETE http://127.0.0.1:20144/api/telemetry`)*

#### Contoh Response
```json
{
  "status": "cleared",
  "message": "In-memory telemetry and logs have been reset."
}
```

---

### 3.8. `GET /` (Web Dashboard)
Antarmuka web modern responsif berbasis Dark Glassmorphism yang di-*embed* langsung ke binary Go (`//go:embed`).

Buka langsung di browser:
👉 **`http://127.0.0.1:20144/`**

**Struktur Komponen Dashboard:**
1. **6 Stat Header Cards**:
   - `Default Model`: Model fallback aktif (`deepseek-v4.1-flash-free`).
   - `Total Requests`: Total request yang dilayani.
   - `Input & Cache Hit`: Jumlah token input beserta visual progress bar dan persentase Cache Hit.
   - `Output Tokens`: Throughput total token keluaran (jawaban/kode).
   - `Active Sessions`: Sesi aktif dalam cache LRU Session Store.
   - `Daemon Uptime`: Durasi aktif daemon.
2. **Model & Routing Settings Panel**:
   - Dropdown ganti model default + chip pilihan instan (`deepseek-v4.1`, `beam-free`, `glm-5.3`, `mimo`, `nemotron`).
   - Box API Key lokal (`sk-opencode-free`).
   - Master Prompt Tuning Box (editor instruksi artikulasi AI).
   - Monitor status Outbound Proxy Pool (`Ready` vs `Cooldown`).
   - Path file `config.json` lokal + tombol *Save Settings*.
3. **Live Model Tester (Playground)**:
   - Pengujian prompt streaming (SSE) langsung di browser.
   - Pemisahan blok `[Reasoning]` (CoT R1) dan `[Answer]`.
   - Stopwatch latensi eksekusi.
4. **Live Stream Telemetry & Request Inspector Table (Last 50 Requests)**:
   - Tabel audit real-time 50 request terakhir:
     - Waktu request
     - Model (`Requested → Actual`)
     - Lane API (`chat`, `responses`, `messages`)
     - Jumlah Token Input (Prompt)
     - Badge Cache Hit % (`High ≥70%`, `Med ≥30%`, `Low <30%`)
     - Jumlah Token Output
     - Latensi (ms)
     - Status HTTP (`200 OK`, `429`, dll.)
   - Tombol *↻ Refresh* real-time.
5. **Quick Integration Guide**: Snippet koneksi siap pakai untuk Cline, Roo Code, Cursor, dan Aider.

---

### 3.9. `POST /v1/messages`
Endpoint penjaga format Anthropic Claude. Klien yang memanggil endpoint ini akan menerima respon `HTTP 501 Not Implemented` yang mengarahkan mereka untuk menggunakan format standar OpenAI di `/v1/chat/completions`.

```json
{
  "error": {
    "code": "not_implemented",
    "message": "The Anthropic /v1/messages surface is not exposed by this proxy; use /v1/chat/completions with an OpenAI-compatible model id.",
    "type": "not_implemented"
  }
}
```

---

## 4. Cara Integrasi ke Coding Agents & Klien AI

### 4.1. Cline / Roo Code / Cursor
- **API Provider**: OpenAI Compatible
- **Base URL**: `http://127.0.0.1:20144/v1`
- **API Key**: `sk-opencode-free`
- **Model ID**: `deepseek-v4.1-flash-free` *(atau alias `default` / `gpt-4o`)*

### 4.2. Python (Official `openai` SDK)
```python
from openai import OpenAI

client = OpenAI(
    base_url="http://127.0.0.1:20144/v1",
    api_key="sk-opencode-free"
)

response = client.chat.completions.create(
    model="deepseek-v4.1-flash-free",
    messages=[{"role": "user", "content": "Halo! Tulis fungsi fizzbuzz di Python."}],
    stream=True
)

for chunk in response:
    content = chunk.choices[0].delta.content or ""
    print(content, end="", flush=True)
```

### 4.3. Node.js / TypeScript
```typescript
import OpenAI from "openai";

const client = new OpenAI({
  baseURL: "http://127.0.0.1:20144/v1",
  apiKey: "sk-opencode-free",
});

async function main() {
  const stream = await client.chat.completions.create({
    model: "deepseek-v4.1-flash-free",
    messages: [{ role: "user", content: "Halo dunia!" }],
    stream: true,
  });

  for await (const chunk of stream) {
    process.stdout.write(chunk.choices[0]?.delta?.content || "");
  }
}

main();
```
