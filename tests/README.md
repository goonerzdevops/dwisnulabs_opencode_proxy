# Testing & Quality Assurance Suite

Direktori ini mengelompokkan skrip pengujian, payload smoke test, serta artefak logging untuk OpenCode Proxy.

---

## 1. Struktur Pengujian

```
tests/
├── README.md               # Dokumentasi panduan testing ini
├── scripts/                # Kumpulan skrip pengujian & probe PowerShell
│   ├── build_check.ps1     # Otomasi mod tidy, vet, build, dan unit test Go
│   ├── test_lanes.ps1      # Pengujian probe lane header dan contract fingerprint
│   ├── test_smoke.ps1      # Smoke test end-to-end server (health & ping)
│   ├── test_matrix.ps1     # Probe pengujian chat multi-model
│   └── probe_env.ps1       # Pemeriksaan environment & toolchain Go
├── smoke/                  # Fixture, konfigurasi mock, dan payload uji coba
│   ├── config.json         # Konfigurasi sandbox untuk smoke test
│   ├── *.json              # Payload chat request / response
│   └── test_models.ps1     # Helper pengujian model
└── logs/                   # Log output pengujian sementara (diabaikan oleh git)
```

---

## 2. Unit Testing Go (Co-located Idiom)

Sesuai standar arsitektur bahasa Go (*Go Standard Layout*), unit test diletakkan berdampingan (*co-located*) di dalam folder package yang diuji:
- **`internal/proxy/puter_test.go`**: Pengujian integrasi Puter.js, deteksi model, validasi token, failover multi-akun (HTTP 402 / 429), serta reset/delete akun pool.
- **`internal/proxy/transform_test.go`**: Pengujian transformasi format request/response OpenAI compatible.
- **`internal/proxy/transport_test.go`**: Pengujian client transport, roundtripper, dan connection pool.
- **`internal/proxy/telemetry_test.go`**: Pengujian WAL logger dan kalkulasi token usage.
- **`internal/proxy/zen_v2_test.go`**: Pengujian multi-provider hybrid gateway `/v2`.

### Cara Menjalankan Unit Test:
```powershell
# Jalankan seluruh unit test
go test -v ./...

# Jalankan khusus pengujian Puter
go test -v ./internal/proxy -run "TestPuter"
```

---

## 3. Menjalankan Skrip Pengujian

```powershell
# Validasi build, vet, dan test
.\tests\scripts\build_check.ps1

# Jalankan probe lane headers
.\tests\scripts\test_lanes.ps1

# Jalankan smoke test server
.\tests\scripts\test_smoke.ps1
```

