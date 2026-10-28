# Cara Kerja "OpenCode" di 9Router Proxy

> Analisis dari source code langsung (9router v0.5.104, clone `decolua/9router` @87a74e1) + live PR GitHub per 17 Oktober 2026.
> Semua klaim dibawah punya rujukan file/baris atau nomor PR.

---

## 1. Ringkasan

"OpenCode di 9router" itu ada **dua arah berbeda** yang sering tertukar:

| Arah | Peran 9Router | Yang ditelusuri |
|---|---|---|
| **A. OpenCode sebagai CLIENT** | 9Router jadi gateway OpenAI-compatible untuk OpenCode CLI | Config yang 9Router tulis ke `opencode.json` |
| **B. OpenCode sebagai PROVIDER** ("OpenCode Free" / Zen / Go) | 9Router meneruskan request client manapun (Hermes, Claude Code, dll.) ke free tier `opencode.ai` | `open-sse/executors/opencode*.js` |

Arah B yang paling teknis dan paling sering berubah (banyak PR Oktober 2026) karena opencode.ai **mengunci free tier-nya** — dan 9Router terus berkejaran mempertahankan fingerprint client.

---

## 2. Arah A — OpenCode CLI menunjuk ke 9Router

Sumber: `cli/src/cli/commands/connectTools.js:183-237`

Perintah `9router connect opencode` menulis `~/.config/opencode/opencode.json`:

```json
{
  "provider": {
    "9router": {
      "npm": "@ai-sdk/openai-compatible",
      "options": {
        "baseURL": "http://localhost:20128/v1",
        "apiKey": "<9router-key>"
      },
      "models": { "<model>": { "name": "<model>", ... } }
    }
  },
  "model": "9router/<model>",
  "agent": {
    "explorer": { "mode": "subagent", "model": "9router/<model>" }
  }
}
```

Catatan implementasi:
- Provider memakai `@ai-sdk/openai-compatible` — satu format wire OpenAI untuk semua model, termasuk model yang di-upstream berformat Anthropic/Gemini (9Router yang mentranslate).
- `agent.explorer` diarahkan ke model yang sama — subagent eksplorasi ringan jalan di model yang dipilih.
- `reset()` menghapus hanya entri `9router` — config lain di `opencode.json` tidak disentuh.
- Daftar tool yang didukung 9Router CLI: `claude, codex, opencode, droid, crush, kilo, cline, pi, omp` (line 538); config JSONC ditoleransi di semua 8 route settings (CHANGELOG).

Setelah itu arah request OpenCode:

```
OpenCode CLI/agent
  → POST http://localhost:20128/v1/chat/completions   (format OpenAI)
  → 9Router: combos + fallback + RTK (kompres tool output) + format translator
  → provider apa pun yang terpilih (bisa juga OpenCode Free, lihat arah B)
```

---

## 3. Arah B — 9Router sebagai client ke opencode.ai ("provider OpenCode")

Ada **tiga provider terpisah** di registry (`open-sse/providers/registry/`):

| Provider | Basemap upstream | Format model | Catatan |
|---|---|---|---|
| `opencode` — "OpenCode Free" | `https://opencode.ai` | muse-spark → `openai-responses`, union-alpha → `claude`, jev → systemone | noAuth (`Bearer public`), free tier |
| `opencode-zen` — "OpenCode Zen" | `https://opencode.ai/zen/v1/responses` | keyed lane (API key) untuk model `-free` | fingerprint quartet |
| `opencode-go` — "OpenCode Go" | `https://opencode.ai/zen/go/v1/responses` | 40+ model (glm, kimi, deepseek, qwen, grok, gpt...) | catalog auto-fetch |

Katalog model di-fetch live dari `https://opencode.ai/zen/v1/models` (`modelsFetcher`), jadi model baru muncul tanpa update 9Router.

### 3.1 Alur request (OpenCode Free, `executors/opencode.js`)

1. **`prepareRequestCredentials`** — resolusi session, menghasilkan `_opencodeSession` yang stabil.
2. **`transformRequest`** — normalisasi body:
   - **selalu `stream: true`** ke upstream (`body.stream = true`). Zen menolak non-streaming di free model dengan 403 FreeTierError; kalau client downstream minta non-stream, handler layer yang mengagregasi SSE jadi JSON utuh.
   - model muse-spark → jalur **Responses API**: `max_tokens/max_completion_tokens` → `max_output_tokens`, `reasoning_effort` → `reasoning:{effort,summary}`, `store: false`, tool_choice dipaksa `auto` untuk muse-spark-1.3 (`forceAutoToolChoiceModels`), dan **items `reasoning` dari turn sebelumnya dibuang** (lihat 3.3).
3. **`applyFingerprintTools`** — injeksi/cloaking tool fingerprint (lihat 3.2).
4. **`buildUrl(model)`** — endpoint per format:

| Model | Endpoint upstream |
|---|---|
| muse-spark-1.2/1.3-contributor-free | `https://opencode.ai/inference/v1/responses` |
| union-alpha | `…/inference/v1/messages` (Anthropic wire, header `anthropic-version`) |
| lainnya | `…/inference/v1/chat/completions` |

   Sejak PR #4842 (v0.5.104, 2026-10-15): free tier **pindah dari `/zen/v1` ke `/inference/v1`**; `/zen/v1` sekarang menjawab SEMUA request free dengan 403 FreeTierError.
5. **`buildHeaders`** — header pemalsuan identitas client OpenCode:

```
Authorization: Bearer public
User-Agent: opencode/latest/2.0.26/cli     (v2 wajib — UA v1 "opencode/1.18.x"
                                            di-403 FreeTierError sejak 2026-10-14)
x-opencode-client: cli
x-opencode-session / x-opencode-session-id / x-session-affinity / x-session-id:
    <ses_...>  (session canonical, lihat 3.4)
x-opencode-project: global
```

### 3.2 Tool fingerprint (alasan paling "menarik")

Sumber: `open-sse/utils/opencodeFingerprint.js:1-33`, executor zen menulis `["bash","glob","grep","read"]` (PR #4132).

Upstream free tier **memvalidasi bahwa request mendeklarasikan tool internal OpenCode sendiri** — `read` dan `shell` pada lane v2 — dengan **deskripsi dan JSON schema persis verbatim** dari CLI OpenCode v2.0.26. Deskripsi di-stub, nama di-rename, atau `tool_choice: "none"` → langsung **403 FreeTierError**.

Aturan:
- Tool `read`/`shell` milik client dipetakan via WeakMap request-lokal: jika nama collision dengan fingerprint tapi definisinya beda → dikirim sebagai **alias** dan dinama-kan balik di sisi response.
- Tool caller lain dikirim utuh.
- Fingerprint wajib walau client sudah kirim tool sendiri (contoh di komentar: ZCode/Claude Code biasanya punya array tool non-empty — skipping → 403).

PR terkait masih mengalir: **#4972** "normalize colliding fingerprint tool definitions in place" (2026-10-16) dan **#4883** "refresh the client fingerprint to the 2.0 channel form".

### 3.3 Engineering session (kenapa 9Router men-cache session)

Sumber: `executors/opencode.js:43-154` (dan duplikat di zen/go).

Format `x-opencode-session` yang diterima upstream harus persis:

```
ses_<12 hex yang men-encode ~(timeMs*4096+counter) 48-bit><14 char base62 random>
```

- Upstream **men-decode prefix itu**: counter melebihi ~16–100 (dari probe live) → 403 FreeTierError "non-small counter". Karena itu `generateSessionId()` menjaga counter asli per-timestamp (perilaku CLI asli), bukan angka random.
- **Kuota free tier upstream di-faktur per session.** Session baru per request → 429 `FreeUsageLimitError` dengan reset-after memanjang. CLI asli memakai SATU session long-lived per percakapan.
- 9Router meniru: **`stableSessionId(credentials)`** — satu session canonical per identitas downstream (key: `connectionId` atau hash auth header), dipinjam kembali tiap request, evict LRU setelah TTL (`MEMORY_CONFIG.sessionTtlMs`, max 1000 entri).
- Session dari client yang tidak berformat opencode (mis. session Hermes) di-translate deterministik via SHA-256 (`translateSessionId`) — counter dipin ke 1, sama seperti yang CLI asli kirim.

PR lama yang jadi dasar: v0.5.x "resolve 403 FreeTierError … dengan canonical session format, valid UA, dan stable upstream session reuse" (CHANGELOG line 145-168).

### 3.4 Perbaikan upstream yang paling aktif

Dari komentar source + PR minggu ini (16-17 Okt 2026):

| PR / perbaikan | Isi |
|---|---|
| **#4988** (2026-10-17) | Bila relay/proxy 9Router kena 403 FreeTierError, replay request sekali lewat egress langsung (nempel dengan SOCKS5/egress baru #4987) |
| **#4922** | Reasoning "adaptive" dari 400 + turn Responses terpotong di muse-spark |
| **#4885** | Pisah reasoning summary parts di Responses API dengan blank line |
| **#4984** | Honor `low\|high\|max` reasoning effort untuk zen free deepseek-v4.1-flash |
| **#4876** | opencode-go forward Claude beta flags + adaptive thinking `claude-haiku-5-5` via `/messages` |
| **#4991** | opencode-go dapat wire transport **Gemini** + model `gemini-4-argon-contributor` |
| CHANGELOG (#4805) | Zen menolak multi-turn yang assistant message tanpa `reasoning_content` (400 missing_required_parameter) → 9Router inject placeholder reasoning |
| CHANGELOG (#4780) | Session hasil translate punya "inverted timestamp" agar client non-OpenCode lolos free-tier check |

Jadi polanya konsisten: **opencode.ai memasang sekat (fingerprint, counter, encrypted reasoning) → 9Router meniru identitas CLI asli + meng-cache state untuk menembus.**

### 3.5 Konsekuensi ke model Responses API (muse-spark)

Karena free tier memakai `Bearer public` yang routing ke **akun upstream yang di-pool** (bukan account kamu), OpenAI Responses API menolak `encrypted_content` reasoning yang di-encrypt oleh akun lain:

> "reasoning `encrypted_content` was not issued to this caller (400)"

Solusi 9Router (source comment lines 270-279): buang semua item `reasoning` dari input + `store: false` + strip `encrypted_content`. Ini yang memungkinkan multi-turn + tool-calling loop jalan tanpa 400.

---

## 4. Implikasi praktis untuk setup kamu (Hermes → 9Router)

1. **Model gratis yang bisa dipakai langsung**: `oc/muse-spark-1.3-contributor-free` atau `oc/jev-1.13-free` (alias `oc`) masuk arah B otomatis — Hermes + combo tinggal memakainya, gratis, kualitas ringan.
2. **Waspada keberlanjutan**: arah B bergantung pada peniruan fingerprint versi CLI; opencode.ai bisa menaikkan gate itu kapan saja (contoh: gerbang `/zen/v1 → /inference/v1` tanggal 2026-10-14). 9Router mengikuti lewat PR, tapi selalu ada jeda downtime sesaat saat gate berubah.
3. **Kuota per-session**: satu canonical session per identitas downstream dipakai ulang dari cache (`sessionTtlMs`), meniru perilaku CLI asli. Jangan sering restart 9Router saat kosong — cache session hangus dan kuota free tier mulai dari nol lagi.
4. **Arah A tidak pernah bermasalah**: config `opencode.json` stabil mengikuti format OpenAI-compatible, tidak terpengaruh drama fingerprint.
5. **RAM**: semua executor ini Node.js in-memory — Map session + fetch pool, tanpa binary tambahan. Dampak ke RAM 3.8GB WSL kamu: nol berarti, tetap di dalam proses 9router yang sudah resident.

---

## 5. Referensi singkat

- `open-sse/executors/opencode.js`, `opencode-zen.js`, `opencode-go.js`, `base.js`
- `open-sse/providers/registry/opencode.js`, `opencode-zen.js`, `opencode-go.js`
- `open-sse/utils/opencodeFingerprint.js`
- `cli/src/cli/commands/connectTools.js` (routes config 9 tool client)
- CHANGELOG v0.5.x (baris #427-4842), PR decolua/9router #4991, #4988, #4984, #4972, #4922, #4883, #4876
- sst/opencode — schema config `packages/schema/src/config/provider.ts` mendukung custom provider via `npm` + `options.baseURL`

*Dibuat Hermes, 17-10-2026. Analisis statis (baca source + changelog/PR); belum mem-fire request live ke opencode.ai.*
