# Implementation Plan: Refactor API Gateway to Go (`refactor/golang`)

## Overview

Migrasi core API gateway (`/v1/*`) dari runtime Next.js / Node.js ke Go backend berkemampuan tinggi. Next.js dipertahankan untuk menyajikan UI dashboard dan administrasi, sementara Go menangani routing traffic AI, format translation, SSE streaming, token saver compression, load balancing, dan endpoint `/v1/quota`.

## Architecture Decisions

- **Hybrid Core Gateway Model**:
  - Daemon Go berjalan di port internal (cth: `20129`) atau mengambil alih port publik `20128`, sementara Next.js berjalan di port sekunder (`20127`).
  - Go menangani endpoints performa tinggi: `/v1/chat/completions`, `/v1/models`, `/v1/quota`, `/v1/embeddings`, dll.
  - Next.js tetap melayani SSR / App router dashboard dan mengarahkan calls gateway langsung ke daemon Go.
- **Framework & Router Go**:
  - `chi` (v5) untuk routing HTTP berkecepatan tinggi, middleware context timeout, dan streaming SSE tanpa alokasi buffer berlebih.
  - Zero heavy third-party framework; idiomatic standard library `net/http`.
- **Database Sharing**:
  - SQLite yang sama (`~/.9router/` atau `DATA_DIR`) diakses secara concurrent menggunakan Go driver `modernc.org/sqlite` (pure Go tanpa CGO) atau `github.com/mattn/go-sqlite3`.

---

## Task List

### Phase 1: Branch & Git Preparation

- [ ] Task 1.1: Commit semua fitur baru di `master` (`feat(auth): add username and totp 2fa`, `feat(quota): add v1 quota endpoint`, `feat(dashboard): usage ui enhancement`).
- [ ] Task 1.2: Buat dan checkout branch baru `refactor/golang`.

### Phase 2: Go Gateway Core Foundation

- [ ] Task 2.1: Inisialisasi Go module (`go mod init 9router-core`) di direktori `gateway/` atau `server/`.
- [ ] Task 2.2: Setup database layer SQLite di Go (model schema: API keys, Connections, Settings, Usage).
- [ ] Task 2.3: Implementasi middleware autentikasi (Bearer API key validation & Peer IP verification) di Go.

### Phase 3: High-Performance Endpoints (`/v1/*`)

- [ ] Task 3.1: Implementasi `/v1/models` di Go.
- [ ] Task 3.2: Implementasi `/v1/quota` di Go dengan concurrent provider usage fetching via goroutines.
- [ ] Task 3.3: Implementasi SSE streaming core untuk `/v1/chat/completions` dengan adapter OpenAI/Claude/Kiro.

### Phase 4: Integration & Next.js Rewrites

- [ ] Task 4.1: Konfigurasi `next.config.mjs` untuk reverse-proxy `/v1/*` ke Go gateway daemon.
- [ ] Task 4.2: Update CLI launcher (`cli/`) untuk menyalakan biner Go bersamaan dengan server Next.js.
- [ ] Task 4.3: Jalankan benchmark throughput dan uji kompatibilitas IDE (Cursor/Claude Code).

---

## Verification

- Unit test Go: `go test ./...`
- Regression test: `tests/__baseline__/verify-no-regression.mjs`
- API check: Request curl ke `/v1/quota` dan `/v1/chat/completions`.
