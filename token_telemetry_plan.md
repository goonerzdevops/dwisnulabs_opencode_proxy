# Implementation Plan: Token Telemetry, Cache Hit Analytics & Stream Model Inspector

**Proyek:** OpenCode Proxy (`opencode_proxy`)  
**Target:** Embedded Web UI Dashboard (`//go:embed`), SSE Stream Interception, & Token/Cache Analytics  
**Status:** Draf Rekomendasi & Rencana Implementasi Produksi  
**Tanggal:** 19 Oktober 2026  

---

## 1. Latar Belakang & Identifikasi Kebutuhan

Pada antarmuka Web Dashboard bawaan OpenCode Proxy (di-embed langsung ke binary Go via `//go:embed`), saat ini metrik yang tampil pada header cards baru mencakup:
- `DEFAULT MODEL`
- `TOTAL REQUESTS`
- `ACTIVE SESSIONS`
- `DAEMON UPTIME`

### Informasi Kritis yang Perlu Ditambahkan:
1. **Input Tokens (Prompt Tokens):** Total token prompt yang dikirim oleh client (Cline/Cursor/WebUI).
2. **Cache Hit Percentage (%):** Persentase token input yang berhasil di-*hit* dari KV cache / prompt cache upstream.
3. **Output Tokens (Completion Tokens):** Total token jawaban yang dihasilkan oleh LLM.
4. **Model LLM Riil yang Berjalan pada Stream:** Model upstream sesungguhnya yang melayani request (menangkap fallback routing atau pemetaan alias `default` -> model spesifik).
5. **Riwayat Permintaan & Stream Telemetry Table:** Tabel inspeksi request real-time untuk memantau performa streaming (latency, TTFT, cache hit, token throughput).

---

## 2. Analisis Database: Butuh SQLite atau In-Memory Ring Buffer?

### 2.1 Perbandingan Opsi Penyimpanan

| Kriteria | Opsi A: Pure Go In-Memory Ring Buffer + WAL (REKOMENDASI) | Opsi B: Pure Go SQLite (`modernc.org/sqlite`) | Opsi C: Client-Side IndexedDB (Browser JS) | Opsi D: CGO SQLite (`mattn/go-sqlite3`) |
| :--- | :--- | :--- | :--- | :--- |
| **Dependensi Eksternal** | **0 (Murni Go stdlib)** | Menambah 1 Go module (`modernc.org/sqlite`) | 0 (Native Browser Web API) | Butuh GCC / MinGW & CGO |
| **Ukuran Binary** | **0 KB tambahan** (~15 MB total) | +8 s/d 12 MB (~25 MB total) | 0 KB di server | +6 MB, kompilasi CGO rumit |
| **Kecepatan Read/Write** | **< 0.001 ms (Nanodetik di RAM)** | ~0.5 - 2 ms (File I/O disk) | Cepat (Local browser) | ~0.5 - 1 ms |
| **Persistensi Data** | Append-only JSONL ke `logs/telemetry.jsonl` | File database `telemetry.db` | Hilang jika clear browser cache | File `telemetry.db` |
| **Kebutuhan RAM** | **< 2 MB** untuk 1.000 log terakhir | 15 - 30 MB (SQLite buffer) | 0 MB di server | 15 - 25 MB |
| **Kesesuaian Desain** | **Sangat Sesuai** dengan filosofi *zero-dependency* proxy | Bagus jika butuh query SQL agregat berat | Bagus untuk audit client | **Hindari** (merusak portabilitas Windows/Linux) |

### 2.2 Keputusan Rekomendasi Database
1. **Gunakan Opsi A (In-Memory Ring Buffer + JSONL WAL di Go):**
   - Server menyimpan **500 - 1.000 request terakhir** dalam circular buffer thread-safe (`sync.RWMutex`).
   - Counter total kumulatif (`atomic.Int64` untuk input tokens, cached tokens, output tokens, total requests).
   - Secara asinkron meng-append log baris demi baris ke file `logs/usage.jsonl` (format append-only, zero lock contention).
   - Saat proxy restart, proxy membaca baris ringkasan terakhir untuk memulihkan counter kumulatif.
2. **Jika Pengguna Membutuhkan Query SQL Khusus:**
   - Gunakan `modernc.org/sqlite` (Pure Go tanpa CGO). Tetapi untuk kebutuhan monitoring proxy, Ring Buffer + JSONL jauh lebih ringan, cepat, dan tidak berisiko korupsi database.

---

## 3. Analisis Library JavaScript: Mana yang Paling Ultra Ringan?

Web UI OpenCode Proxy di-embed ke dalam binary Go. Mengingat dashboard saat ini berukuran sangat kecil (`app.js` ~10 KB, `index.html` ~5 KB), penambahan library JS harus mempertimbangkan ukuran dan efisiensi.

### 3.1 Perbandingan Library JS untuk Visualisasi & Tabel

| Library | Ukuran (Min + Gzip) | Tipe / Kegunaan | Kelebihan | Rekomendasi |
| :--- | :--- | :--- | :--- | :--- |
| **Pure Vanilla JS (Canvas 2D / Inline SVG)** | **0 KB (Nol dependensi)** | Mini sparkline charts, progress bar, cache donut, log table | 100% native browser, instan, zero HTTP load, desain bisa dikustomisasi penuh sesuai neon dark-mode. | **Pilihan Utama (Super Ultra Ringan)** |
| **uPlot** | **~12 KB** | Time-series chart (Token throughput & Cache Rate) | Library grafik berbasis Canvas tercepat di dunia, memori minimal, 15x lebih cepat dari Chart.js. | **Pilihan Terbaik jika Butuh Interactive Chart** |
| **Grid.js / Tabulator** | ~15 - 45 KB | Tabel data | Fitur paging & sorting otomatis. | *Opsional* (Vanilla JS table sudah cukup untuk 50 baris). |
| **Chart.js** | ~60 - 70 KB | Chart multi-purpose | Populer, namun terlalu besar (*bloated*) untuk dashboard embedded ringan. | **Tidak Direkomendasikan** |

---

## 4. Algoritma Perhitungan Token & Cache Hit

### 4.1 Apakah Memungkinkan?
**YA, 100% MEMUNGKINKAN DAN SANGAT AKURAT.**
OpenCode upstream (baik OpenAI Chat Completions endpoint maupun Responses API) telah menyertakan telemetri token di dalam stream SSE.

### 4.2 Struktur Data Telemetri Upstream
Pada chunk penutup stream atau event `response.completed`, upstream mengirimkan payload:
```json
{
  "id": "chatcmpl-b2ca17c1",
  "model": "deepseek-v4.1-flash-free",
  "usage": {
    "prompt_tokens": 1420,
    "completion_tokens": 380,
    "total_tokens": 1800,
    "prompt_tokens_details": {
      "cached_tokens": 1152
    },
    "completion_tokens_details": {
      "reasoning_tokens": 96
    }
  }
}
```

### 4.3 Rumus & Algoritma Perhitungan

1. **Perhitungan Cache Hit Rate (Persentase):**
   $$\text{Cache Hit Rate (\%)} = \begin{cases} \left( \frac{\text{cached\_tokens}}{\text{prompt\_tokens}} \right) \times 100\%, & \text{jika prompt\_tokens} > 0 \\ 0\%, & \text{jika prompt\_tokens} = 0 \end{cases}$$
   *Contoh:*
   - `prompt_tokens` = 1.420
   - `cached_tokens` = 1.152
   - $\text{Cache Hit Rate} = (1152 / 1420) \times 100\% = \mathbf{81.13\%}$

2. **Perhitungan Fresh (Uncached) Tokens:**
   $$\text{fresh\_prompt\_tokens} = \text{prompt\_tokens} - \text{cached\_tokens}$$
   *(1.420 - 1.152 = 268 token baru yang diproses GPU).*

3. **Perhitungan Total Token Efficiency Savings:**
   $$\text{Efficiency Savings (\%)} = \left( \frac{\text{cached\_tokens}}{\text{total\_tokens}} \right) \times 100\%$$

4. **Identifikasi Model LLM Asli yang Berjalan:**
   - Ambil nilai string `model` dari chunk SSE upstream pertama atau chunk penutup.
   - Bandingkan dengan model yang diminta client (`requested_model` vs `actual_model`).
   - Simpan nama model riil untuk ditampilkan di dasbor.

### 4.4 Metode Intersepsi Stream di Proxy Go
- **Pada `LaneResponses` (`ResponsesBridge`):**
  - Event `response.completed` sudah mengekstrak `b.usage` dan `b.modelEcho`.
  - Pasang hook di akhir `Finish()` untuk mengirimkan data ke `telemetry.Collector`.
- **Pada `LaneChat` (`serveChatLane`):**
  - Untuk non-streaming: `ChatAggregator` sudah menangkap `a.usage` dan `a.modelEcho`.
  - Untuk streaming (`prepared.ClientStreams == true`):
    - Ganti `io.Copy` mentah dengan `TeeStreamInterceptor` ringan yang membaca SSE chunk baris demi baris, meneruskannya ke client seketika (tanpa delay), dan mengurai JSON jika menemukan kata kunci `"usage":` atau chunk sebelum `data: [DONE]`.

---

## 5. Rekomendasi Tampilan Dashboard & Mockup UX

### 5.1 Perluasan Stat Cards Header (Grid 6 Kolom)

Susunan kartu metrik utama diubah dari 4 kartu menjadi 6 kartu (tampilan 3x2 pada desktop atau 2x3 pada layar medium):

```
+-----------------------------------+-----------------------------------+-----------------------------------+
| DEFAULT MODEL                     | TOTAL REQUESTS                    | INPUT & CACHE HIT                 |
| deepseek-v4.1-flash-free          | 274                               | 1.84M                             |
| Upstream: Responses & Chat        | Served through gateway            | [===== 81.4% Hit =====][ 18.6% ]  |
|                                   |                                   | 1.50M cached / 340k fresh         |
+-----------------------------------+-----------------------------------+-----------------------------------+
| OUTPUT (COMPLETION)               | ACTIVE SESSIONS                   | DAEMON UPTIME                     |
| 412K                              | 108                               | 1d 10h 58m                        |
| Avg ~38.4 tokens/s                | Canonical affinity cached         | System service status             |
+-----------------------------------+-----------------------------------+-----------------------------------+
```

#### Detail Desain Visual Kartu Baru:
1. **Kartu `INPUT & CACHE HIT`:**
   - Nilai Utama: `1.84M` (font mono cyan `#38bdf8`)
   - Progress Bar Two-Tone:
     - Hijau Neon (`#10b981`): 81.4% (Cached)
     - Abu-abu Slate (`#475569`): 18.6% (Fresh Input)
   - Subtext: `🟢 81.4% Cache Hit (1.50M cached)`
2. **Kartu `OUTPUT (COMPLETION)`:**
   - Nilai Utama: `412K` (font mono purple/indigo `#818cf8`)
   - Subtext: `Total generation throughput`

---

### 5.2 Live Stream Telemetry & Request Inspector (Tabel Baru)

Diletakkan di bawah baris Playground dan Model Settings. Menampilkan tabel interaktif yang memperlihatkan request yang sedang atau baru saja lewat:

```
+---------------------------------------------------------------------------------------------------+
| 📊 Live Stream Telemetry & Request Inspector (Last 50 Requests)             [Auto-Refresh: ON] [Clear] |
+----------+----------------------------+-----------+--------------+--------------+---------+--------+
| TIME     | MODEL (REQUESTED -> ACTUAL)| LANE      | PROMPT TOKENS| CACHE HIT %  | OUTPUT  | LATENCY|
+----------+----------------------------+-----------+--------------+--------------+---------+--------+
| 22:18:40 | deepseek-v4.1-flash-free   | chat      | 2,450        | [86.4% HIT]  | 420     | 580 ms |
| 22:17:15 | gpt-4o -> beam-free        | chat      | 1,120        | [42.1% HIT]  | 180     | 320 ms |
| 22:15:02 | muse-spark-1.2-contributor | responses | 6,800        | [94.2% HIT]  | 890     | 1.25 s |
+----------+----------------------------+-----------+--------------+--------------+---------+--------+
```

#### Fitur Tabel:
- **Pill Badge Cache Hit:**
  - Hijau Glow (`#10b981`): > 70% Cache Hit
  - Kuning Amber (`#f59e0b`): 30% - 70% Cache Hit
  - Abu-abu Muted (`#64748b`): < 30% atau Cache Miss
- **Indikator Model:** Jika client meminta alias `gpt-4o`, tabel menampilkan `gpt-4o → deepseek-v4.1-flash-free` sehingga pengguna langsung paham proses routing proxy.
- **Paging / Limit:** Menampilkan 20 - 50 request terakhir secara dinamis tanpa lag.

---

### 5.3 Pembaruan pada Live Model Tester (Playground)

Di bawah terminal SSE Stream Output pada Playground, tambahkan bar telemetri komprehensif setelah stream selesai:

```
[SEBELUM]
Completed in 842 ms | Model: deepseek-v4.1-flash-free

[SESUDAH]
Completed in 842 ms | Model: deepseek-v4.1-flash-free
Tokens: In: 1,420 (Cache Hit: 81.1% / 1,152 cached) | Out: 380 | Total: 1,800
Speed: ~451 tok/s | Upstream Lane: chat
```

---

## 6. Arsitektur Teknis Backend (Go)

```
[ Client Request ]
       │
       ▼
[ Server HTTP Mux (/v1/chat/completions) ]
       │
       ▼
[ Prepare Request (Lane & Model Resolution) ]
       │
       ▼
[ Upstream Send (HTTP Streaming) ]
       │
       ├─► [ Stream Parser / SSE Bridge ]
       │         │ (Intercept 'usage' & 'model' chunks)
       │         ▼
       │   [ Telemetry Collector (In-Memory Ring Buffer) ]
       │         │
       │         ├─► Aggregate Totals (Atomic Int64)
       │         ├─► Circular Buffer (Recent 500 requests)
       │         └─► Async Append to logs/usage.jsonl
       │
       ▼
[ Stream Passthrough to Client ]
```

### 6.1 Struktur Data Go (`internal/proxy/telemetry.go`)

```go
package proxy

import (
	"sync"
	"sync/atomic"
	"time"
)

type RequestRecord struct {
	ID             string    `json:"id"`
	Timestamp      time.Time `json:"timestamp"`
	RequestedModel string    `json:"requested_model"`
	ActualModel    string    `json:"actual_model"`
	Lane           string    `json:"lane"`
	Stream         bool      `json:"stream"`
	PromptTokens   int       `json:"prompt_tokens"`
	CachedTokens   int       `json:"cached_tokens"`
	OutputTokens   int       `json:"output_tokens"`
	TotalTokens    int       `json:"total_tokens"`
	CacheHitRate   float64   `json:"cache_hit_rate"`
	DurationMs     int64     `json:"duration_ms"`
	StatusCode     int       `json:"status_code"`
}

type TelemetryStore struct {
	mu           sync.RWMutex
	records      []RequestRecord
	maxRecords   int
	head         int
	totalPrompt  atomic.Int64
	totalCached  atomic.Int64
	totalOutput  atomic.Int64
	totalReqs    atomic.Int64
}
```

### 6.2 Endpoint API Baru
1. `GET /api/telemetry`
   - Mengembalikan ringkasan total (`total_prompt`, `total_cached`, `total_output`, `overall_cache_hit_rate`) dan daftar 50 request terakhir.
2. Endpoint `/health` diperbarui untuk menyertakan ringkasan token ringan tanpa membebani polling berkala.

---

## 7. Rencana Kerja Implementasi Bertahap (Roadmap)

### Fase 1: Backend Go Telemetry Engine & Stream Interception
- Buat package `internal/proxy/telemetry.go` dengan struktur Ring Buffer thread-safe dan atomic counters.
- Tambahkan intersepsi token `usage` pada `ResponsesBridge` (`streamer.go`) dan `ChatAggregator` (`chat_aggregate.go`).
- Buat `SSEUsageInterceptor` untuk menangani `LaneChat` saat streaming (`prepared.ClientStreams == true`).
- Ekspos endpoint `GET /api/telemetry`.

### Fase 2: Logging & Persistensi Ringan (WAL JSONL)
- Buat writer asinkron ke `logs/usage.jsonl` agar riwayat token tidak hilang saat proxy di-restart.
- Buat mekanisme auto-recovery saat proxy pertama kali boot.

### Fase 3: Pembaruan Frontend UI (`index.html` & `style.css`)
- Tambahkan 2 kartu baru di Grid Stats: `INPUT & CACHE HIT` dan `OUTPUT TOKENS`.
- Tambahkan komponen progress bar dua warna (Cached vs Fresh).
- Tambahkan kontainer tabel `Live Stream Telemetry & Request Inspector`.

### Fase 4: Integrasi Frontend Logic (`app.js`)
- Buat fungsi `loadTelemetry()` dengan polling tiap 3-5 detik atau saat stream selesai.
- Render tabel request dengan format waktu human-readable dan badge warna cache hit.
- Tambahkan tampilan token breakdown pada Live Model Tester.

### Fase 5: Pengujian & Validasi
- Uji dengan client nyata (Cline / Roo Code / Cursor / Curl).
- Verifikasi akurasi persentase cache hit pada multi-turn conversation.
- Pastikan memory leak check: RAM proxy tetap berada di kisaran 10 - 20 MB.

---

## 8. Ringkasan & Kesimpulan

1. **Database:** Tidak memerlukan database eksternal SQLite CGO. Cukup **In-Memory Ring Buffer + Append-Only JSONL** di Go. Menjaga binary tetap single-executable, zero dependency, dan konsumsi RAM < 20 MB.
2. **Library JS:** Gunakan **Pure Vanilla JS / HTML5 Canvas / SVG (0 KB)** untuk menjaga efisiensi embedded asset. Jika di kemudian hari menginginkan grafik time-series interaktif, gunakan **`uPlot` (~12 KB)**.
3. **Algoritma Cache:** Langsung mengekstrak `input_tokens_details.cached_tokens` dan `input_tokens` dari SSE stream upstream, lalu menghitung `(cached / input) * 100%`. Hal ini 100% akurat dan didukung penuh oleh upstream.
4. **Desain UI:** Menambah kartu metrik cache dan tabel stream log interaktif dengan nuansa dark cyber/modern yang terintegrasi secara mulus dengan tema yang sudah ada.
