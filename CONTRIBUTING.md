# Contributing & Development Guide

## Local development setup

Because the Go binary embeds `web/dist/`, you have two workflows depending on whether you are working on backend or frontend code.

### 1. Backend tests and full build

```powershell
# Build frontend assets first (required so embed.FS has current files)
cd web
npm install
npm run build
cd ..

# Run Go test suite
go test ./... -v

# Build binary
go build -o bin/gravlink.exe ./cmd/gravlink
```

### 2. Frontend hot-reload during UI work

Start the Gravlink backend in one terminal, then run Vite's dev server in another. `web/vite.config.ts` proxies `/api` requests and WebSockets to `https://127.0.0.1:8443`:

```powershell
# Terminal 1: start Go server
./bin/gravlink.exe start

# Terminal 2: start Vite dev server
cd web
npm run dev
```

### 3. Hot-reloading Antigravity Hub UI patches

If you only need to adjust mobile overrides for the proxied Antigravity Hub UI (`/hub/`), edit the files directly in `~/.gravlink/patches/`:

- `~/.gravlink/patches/mobile-hub.css`
- `~/.gravlink/patches/mobile-hub.js`

Gravlink serves files from that directory with `Cache-Control: no-store` when present, falling back to `web/public/patches/`. Once verified, copy your changes back into `web/public/patches/` before committing.

## Code guidelines

- **LAN & auth boundaries first**: Any new API route must pass through `IPFilterMiddleware` and `Sessions.RequireAuth`, and mutating routes must remain protected by CSRF and `Origin` checks.
- **Read-only workspace explorer**: Do not add file-write or arbitrary shell execution endpoints to `internal/workspace/`.
- **Mobile ergonomics**: Keep touch targets `>= 44x44px` on coarse pointers, account for `env(safe-area-inset-bottom)` and `100dvh`, and keep `Enter` inserting newlines inside mobile prompt textareas (`Ctrl/Cmd+Enter` or tapping Send submits).
- **Verify before committing**: Always run `go test ./... -v`, `cd web && npm run build`, `go build -o bin/gravlink.exe ./cmd/gravlink`, and `./bin/gravlink.exe doctor`.
