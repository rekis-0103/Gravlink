<div align="center">
  <img src="web/public/icon.svg" alt="Gravlink icon" width="56" height="56" />
  <h1>Gravlink</h1>
  <p>
    A LAN-only mobile companion for monitoring and controlling <strong>Antigravity IDE</strong> on a Windows host from your phone over local Wi-Fi.
  </p>
  <p>
    <img src="https://img.shields.io/badge/Go-1.26-00ADD8?style=flat-square&logo=go&logoColor=white" alt="Go 1.26" />
    <img src="https://img.shields.io/badge/React-19-61DAFB?style=flat-square&logo=react&logoColor=black" alt="React 19" />
    <img src="https://img.shields.io/badge/Host-Windows_LAN-0078D6?style=flat-square" alt="Windows LAN" />
    <img src="https://img.shields.io/badge/Binary-Single_Executable-475569?style=flat-square" alt="Single Binary" />
  </p>
</div>

Gravlink runs as a single Go binary with an embedded React PWA. It binds directly to your local network interface, generates a local Root CA for HTTPS/WSS on mobile, and bridges your phone to the active Antigravity session without cloud relays, port forwarding, or third-party tunnels.

## What it does

- **Live agent timeline**: Streams `transcript.jsonl` (hydrated with `transcript_full.jsonl` when steps are truncated) over WebSocket with collapsible thinking blocks, syntax-highlighted code blocks, and tool call chips.
- **Remote prompt & approvals**: Start new sessions, send prompts (with voice dictation where supported by the mobile browser), and approve or reject tool/command confirmations right from your phone.
- **Active status & token tracking**: Shows live running state, elapsed duration, and per-session / aggregate token usage (`input`, `cache_read`, `output`) in a dedicated dashboard.
- **Read-only code explorer**: Browse the active workspace directory tree and inspect syntax-highlighted source files on mobile without write access. Sensitive files (`.env*`, private keys, `.git/config`, credentials) are blocked automatically.
- **Local dev preview proxy**: Scans active local dev server ports on the laptop and proxies them cleanly under your authenticated session so you can test web apps on a real phone.
- **Embedded Hub view & diagnostics**: Proxies the local Antigravity Hub web UI with external CSS/JS patch injection (`~/.gravlink/patches/`) and exposes `gravlink doctor` health checks in the UI.

## How traffic flows

```mermaid
flowchart LR
  Phone["Phone Browser / PWA"] -->|HTTPS + WSS (LAN IP)| Gateway["Gravlink Server :8443<br/>IP Filter + Auth + CSRF"]
  Gateway --> Transcripts["~/.gemini/antigravity/brain/<br/>transcript.jsonl & transcript_full.jsonl"]
  Gateway --> Hub["Antigravity Hub RPC / UI<br/>(hub_*.json + /healthz)"]
  Gateway --> Workspace["Workspace Files<br/>(Read-Only Explorer)"]
  Gateway --> Preview["Local Dev Servers<br/>(localhost:PORT)"]
```

## Quick start

### Prerequisites

- Windows 10/11 running **Antigravity IDE** (`agy.exe`)
- Go 1.26+
- Node.js 20+ (for building the frontend bundle)

### Build and run

```powershell
# 1. Build the frontend bundle (embedded into the Go binary via web/embed.go)
cd web
npm install
npm run build
cd ..

# 2. Build the single executable
go build -o bin/gravlink.exe ./cmd/gravlink

# 3. Check Antigravity daemon & transcript compatibility
./bin/gravlink.exe doctor

# 4. Start the LAN server (asks for a host password on first run and prints a QR code)
./bin/gravlink.exe start
```

On startup, Gravlink prints a one-time pairing URL and terminal QR code valid for 5 minutes. Scan it with your phone on the same Wi-Fi network, or press `Enter` in the terminal anytime to generate a fresh code.

### Phone HTTPS & Windows Firewall setup

- **Windows Firewall**: If your phone cannot reach the server over Wi-Fi, allow TCP port `8443` on the Private profile from an Administrator PowerShell (Gravlink prints the exact `netsh` command on startup when no rule is found).
- **Local Root CA**: Self-signed certificates block WebSockets and PWA install on iOS/Android unless trusted. Download the CA from `https://<laptop-ip>:8443/ca.crt` (also linked on the login page and Status tab) and install/trust it on your phone.

## CLI reference

```text
gravlink start [flags]   Start the LAN web server (default)
gravlink setup [flags]   Set or update the host login password
gravlink doctor [flags]  Verify Antigravity IDE integration contracts
gravlink version         Print version
```

Common flags for `gravlink start`:

| Flag | Default | Description |
| :--- | :--- | :--- |
| `-port` | `8443` | TCP port to listen on |
| `-workspace` | Current directory | Workspace root exposed to the read-only Code Explorer |
| `-ip` | Auto-detected | Override primary LAN IPv4 address used in the pairing QR code |
| `-data-dir` | `~/.gravlink` | Directory for `config.json`, TLS certs, `audit.log`, and `patches/` |
| `-http` | `false` | Serve plain HTTP instead of HTTPS (not recommended; disables PWA/secure cookies) |
| `-allow-all-rfc1918` | `false` | Allow any RFC1918 private IP instead of restricting to active LAN interface subnets |
| `-password` | Interactive prompt | Set host password non-interactively |

## Project layout

```text
cmd/gravlink/        CLI entrypoint (`start`, `setup`, `doctor`)
internal/agy/        Hub discovery, RPC/CLI dispatcher, transcript parser, token usage, doctor
internal/config/     Persistent `~/.gravlink/config.json` loader
internal/proxy/      Reverse proxies for Antigravity Hub and local dev preview ports
internal/security/   IP subnet filter, Argon2id/HMAC auth, rate limiter, local TLS CA, audit log
internal/server/     HTTP/WebSocket router, static SPA serving, `/patches/` handler
internal/workspace/  Read-only directory tree & safe file reader
web/                 React + TypeScript + Vite PWA (`embed.FS` packaged into the binary)
```

## Documentation

- [ARCHITECTURE.md](ARCHITECTURE.md) — Hub discovery, transcript tailing, WebSocket streaming, and patch hot-reloading.
- [SECURITY.md](SECURITY.md) — Threat model, LAN isolation rules, authentication, and workspace safeguards.
- [CONTRIBUTING.md](CONTRIBUTING.md) — Local dev workflow, Vite proxy setup, and test commands.
