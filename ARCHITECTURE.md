# Architecture

Gravlink is designed around one constraint: Antigravity IDE evolves quickly, so remote monitoring and control should rely on stable on-disk and daemon contracts rather than brittle UI scraping.

## Core components

### 1. Antigravity discovery & dispatch (`internal/agy/`)

- **Hub discovery (`discovery.go`)**: Scans `~/.gemini/antigravity/daemon/hub_*.json` for running daemons and probes `http://127.0.0.1:<port>/healthz` before accepting a hub as healthy.
- **Dispatch (`dispatcher.go`)**: Sends new prompts and follow-up turns via Hub RPC (`StartCascade`, `SendUserCascadeMessage`) and forwards interactive approvals/rejections via `HandleCascadeUserInteraction`. Falls back to spawning `agy` CLI when no healthy hub is available.
- **Transcript reader (`transcript.go`)**: Reads `~/.gemini/antigravity/brain/<conv-id>/.system_generated/logs/transcript.jsonl`. Whenever a step includes `truncated_fields`, Gravlink hydrates the complete text from the matching line in `transcript_full.jsonl` (capped at 256 KB for content and 128 KB for thinking).
- **Token aggregator (`usage.go`)**: Summarizes `input_tokens`, `cache_read_tokens`, `output_tokens`, turn counts, and active work duration across recent transcripts for `/api/tokens`.
- **Contract checks (`doctor.go`)**: Verifies data directory paths, `agy.exe`, hub health, transcript readability, workspace root, and external patch paths on demand (`gravlink doctor`) and at startup.

### 2. Real-time streaming (`internal/server/router.go`)

- `/api/ws/conversations/{id}` opens a WebSocket using `github.com/coder/websocket`.
- On connect, it sends a `snapshot` payload of existing steps, then checks file size and modification timestamp every 350ms to emit incremental `delta` payloads.
- Active tail steps (`RUNNING`, `PENDING`, `GENERATING`, `WAITING`, `IN_PROGRESS`, `STREAMING`) hold the cursor back until they settle so partial updates continue streaming cleanly.
- If a mobile browser drops or suspends the WebSocket, `web/src/hooks/useAgentStream.ts` automatically falls back to polling `/api/conversations/{id}/steps?from=<cursor>` and resumes WebSocket retries with backoff.

### 3. Reverse proxies & external UI patches (`internal/proxy/`)

- **Hub proxy (`hub_proxy.go`)**: Routes `/hub/*` to the active local Antigravity Hub and injects `mobile-hub.css` / `mobile-hub.js` into HTML responses.
- **Hot-reload patches**: `/patches/{name}` checks `~/.gravlink/patches/` on disk first and falls back to the copies embedded from `web/public/patches/`. UI tweaks for Antigravity Hub updates can be dropped into `~/.gravlink/patches/` without recompiling `gravlink.exe`.
- **Dev preview proxy (`preview_proxy.go`)**: Scans common local dev server ports (`3000`, `3001`, `4173`, `4200`, `4321`, `5000`, `5173`, `5174`, `8000`, `8080`, `8888`, `9000`, excluding Gravlink's own port) and rewrites HTML root links, `Location` redirects, and client `fetch`/`WebSocket` calls under `/preview/{port}/`.

### 4. Frontend PWA (`web/`)

- Built with React 19, TypeScript, Vite 6, `marked` + `DOMPurify` for sanitized Markdown, and `highlight.js` for code blocks.
- Mobile-first layout uses `100dvh`, `env(safe-area-inset-bottom)`, coarse-pointer touch targets (`>= 44px`), system/light/dark theme switching, and optional audio/vibration/browser alerts when an agent finishes or requests approval.
- Compiled into `web/dist/` and embedded directly into the Go binary via `web/embed.go`.
