# AGENTS.md — Gravlink Architecture, Security & Coding Guidelines

## 1. Project Overview
Gravlink is a local-only (LAN), mobile-first web companion for monitoring and controlling **Antigravity IDE** running on a Windows laptop from a smartphone on the same Wi-Fi network.
- **Host OS**: Windows
- **Backend**: Go (single binary, `net/http`, `embed.FS`, `golang.org/x/crypto/argon2`)
- **Frontend**: React + TypeScript + Vite (PWA, mobile-first layout)

## 2. Non-Negotiable Security Rules (CRITICAL)
Because Gravlink bridges access to an AI agent capable of reading/writing files and executing terminal commands on the host laptop, EVERY code change MUST obey these rules:
1. **Strict LAN-Only Isolation**:
   - NEVER integrate cloud relays, public tunnels (ngrok, cloudflared), or UPnP/NAT-PMP port forwarding.
   - All incoming requests MUST pass through the RFC1918/Local Subnet middleware (`internal/security/ipfilter.go`).
2. **Authentication First**:
   - NEVER expose any workspace file, agent transcript, prompt endpoint, or proxy route without verifying an active, authenticated session (`HttpOnly`, `Secure`, `SameSite=Strict` cookie).
   - Passwords MUST be hashed with Argon2id. Plaintext passwords or weak hashes (MD5/SHA1/SHA256 without KDF) are strictly forbidden.
   - Authentication endpoints MUST enforce strict rate-limiting.
   - State-changing API requests (`POST`, `PUT`, `PATCH`, `DELETE`) MUST validate `Origin`/`Host` and `X-Gravlink-CSRF`.
3. **Workspace File Safety**:
   - Code Explorer in MVP is strictly **READ-ONLY**.
   - Every file path requested by the client MUST be canonicalized (`filepath.Clean` + `filepath.EvalSymlinks`) and verified to reside strictly inside the configured workspace root.
   - Sensitive files (`.env*`, `*.pem`, `*.key`, `id_rsa*`, `.git/config`, credentials) MUST be blocked by default.
4. **Audit Logging**:
   - Every login attempt (success/failure), pairing attempt, sensitive file block, prompt submission, and tool approval/rejection MUST be recorded in the structured JSON audit log.

## 3. Architectural Principles & Antigravity Integration
1. **Resilience Against Antigravity Updates (Adapter Pattern)**:
   - Do NOT hardcode a single port or binary name. Always use `internal/agy/discovery`:
     1. Primary: Read `~/.gemini/antigravity/daemon/hub_<port>.json` and verify PID + `GET /healthz`.
     2. Secondary: Inspect running `agy.exe` / `language_server` process flags (`--hub-port`, `--csrf_token`).
   - Do NOT rely solely on fragile DOM scraping of the internal Jetski web UI.
     - Use `~/.gemini/antigravity/brain/<conv-id>/.system_generated/logs/transcript.jsonl` (and `transcript_full.jsonl` when `truncated_fields` is present) as the primary source of truth for real-time agent monitoring.
     - Keep CSS/JS monkey-patches modular and externally overrideable so UI changes in Antigravity can be patched without recompiling the Go binary.
2. **Mobile-First UX & Browser Policy**:
   - **Browser Support**: Target modern mobile browsers (iOS Safari 17.5+ and Chrome Android 123+), using progressive enhancement (`@supports` / feature detection) for newer CSS features without external polyfills.
   - All interactive touch targets MUST be at least `44x44px` on coarse pointers (`@media (pointer: coarse)`).
   - Respect iOS/Android safe areas (`env(safe-area-inset-bottom)`) and use `100dvh` (dynamic viewport height) so virtual keyboards and home bars never obscure navigation or composers.
   - Pressing `Enter` in a mobile prompt textarea MUST insert a newline; sending a prompt requires tapping the explicit Send button (or `Ctrl+Enter` / `Cmd+Enter` on physical keyboards).

## 4. Development & Verification Commands
- **Run Backend Tests**:
  `go test ./... -v`
- **Build Frontend Bundle**:
  `cd web && npm run build`
- **Build Single Binary**:
  `go build -o bin/gravlink.exe ./cmd/gravlink`
- **Check Antigravity Integration Contract**:
  `./bin/gravlink.exe doctor`
