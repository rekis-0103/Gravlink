# Gravlink

**Gravlink** is a LAN-only, mobile-first PWA companion for monitoring and controlling **Antigravity IDE** on a Windows laptop from your smartphone on the same Wi-Fi network.

## Milestone 1 Implemented Features
- **LAN-Only Security Gateway**: Argon2id password auth, single-use QR pairing token, HMAC-SHA256 `HttpOnly` + `SameSite=Strict` + `Secure` session cookies, CSRF validation, sliding-window auth rate limiting (`429`), strict RFC1918/LAN-subnet IP filtering, automatic Local Root CA + LAN TLS certificate generation, and structured JSON audit logging (`~/.gravlink/audit.log`).
- **Antigravity Discovery & Contract Doctor (`gravlink doctor`)**: Discovers live `agy.exe --hub` daemons via `~/.gemini/antigravity/daemon/hub_*.json` + `/healthz`, inspects `agy.exe` availability, and validates `transcript.jsonl` schema compatibility.
- **Real-Time Agent Stream**: Live WebSocket tailing of `transcript.jsonl` (hydrated with `transcript_full.jsonl` when truncated) rendered in a mobile-first timeline with collapsible thinking blocks and tool-call chips.
- **Read-Only Code Explorer**: Mobile file tree + `highlight.js` syntax-highlighted viewer with path-traversal protection and `.env`/private-key blocking.

## Quick Start

```powershell
# 1. Build frontend bundle
cd web
npm run build
cd ..

# 2. Build single binary
go build -o bin/gravlink.exe ./cmd/gravlink

# 3. Verify Antigravity compatibility
./bin/gravlink.exe doctor

# 4. Start LAN server (prompts to set password on first run, prints QR code)
./bin/gravlink.exe start
```

## Automated Tests

```powershell
go test ./... -v
```
