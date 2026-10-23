# Todo List: Go API Gateway Refactor (`refactor/golang`)

## Phase 1: Git & Branch Setup

- [x] Task 1.1: Commit current modifications on `master`
- [x] Task 1.2: Create and checkout branch `refactor/golang`

## Phase 2: Go Gateway Core Setup

- [x] Task 2.1: Initialize Go module `gateway/` with chi v5 and pure Go SQLite
- [x] Task 2.2: Implement DB repository in Go (api_keys, connections, settings)
- [x] Task 2.3: Implement Auth and IP middleware in Go

## Phase 3: Gateway Endpoints

- [x] Task 3.1: Implement `/v1/models` in Go
- [x] Task 3.2: Implement `/v1/quota` in Go with concurrent goroutine scraping
- [x] Task 3.3: Implement SSE reverse proxy & streaming `/v1/chat/completions`

## Phase 4: Integration & Verification

- [x] Task 4.1: Configure Next.js rewrites to proxy `/v1/*` to Go daemon
- [x] Task 4.2: Update CLI process manager for Go binary lifecycle
- [x] Task 4.3: Verify all test suites and IDE status bar endpoints
