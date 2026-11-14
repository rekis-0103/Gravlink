# Security Model

Because Gravlink connects a mobile browser to a host machine capable of running an AI coding agent, it follows a strict LAN-only security model.

## Threat boundaries

Gravlink assumes:
- Your laptop and phone share a trusted local Wi-Fi network.
- Any public internet exposure (cloud relays, port forwarding, ngrok/cloudflared tunnels, UPnP) is explicitly out of bounds.
- Clients on the same subnet must still authenticate before accessing transcripts, workspace files, proxies, or prompt endpoints.

## Implemented controls

| Layer | Implementation | Location |
| :--- | :--- | :--- |
| **Network isolation** | Middleware drops non-private IPs and restricts access to active host LAN interface subnets by default (loopback always allowed). Client IP is read from `RemoteAddr` only; `X-Forwarded-For` is ignored. | `internal/security/ipfilter.go` |
| **Transport encryption** | Generates a 5-year Local Root CA (`~/.gravlink/certs/ca.crt`) and 90-day leaf certificates with SANs matching active LAN IPs. Auto-refreshes when LAN IPs change or within 7 days of expiry. | `internal/security/tls.go` |
| **Password storage** | Host password is hashed with Argon2id. Plaintext passwords are never persisted. | `internal/security/auth.go` |
| **Pairing & sessions** | Terminal QR pairing uses single-use tokens with a 5-minute TTL. Authenticated sessions receive an HMAC-SHA256 cookie (`HttpOnly`, `SameSite=Strict`, `Secure` when TLS is enabled). | `internal/security/auth.go`, `cmd/gravlink/main.go` |
| **CSRF & CSWSH defense** | State-changing requests (`POST`, `PUT`, `PATCH`, `DELETE`) require a valid `X-Gravlink-CSRF` header matching session claims. `Origin` is validated against `Host` on API mutations, proxies, and WebSockets. | `internal/security/auth.go`, `internal/security/ipfilter.go` |
| **Brute-force protection** | `/api/auth/login` and `/api/auth/pair` enforce a sliding-window rate limiter (5 failed attempts within 15 minutes per client IP returns `429`). | `internal/security/ratelimit.go`, `internal/server/router.go` |
| **Workspace containment** | Code Explorer is strictly read-only. Requested paths are cleaned, symlink-resolved (`filepath.EvalSymlinks`), and checked with `filepath.Rel` to ensure they stay inside the workspace root. | `internal/workspace/explorer.go` |
| **Sensitive file blocking** | `.env`, `.env.*`, `*.pem`, `*.key`, `*.p12`, `*.pfx`, `id_rsa*`, `id_ed25519*`, `.git/config`, `.git/credentials`, `credentials.json`, `secrets.json`, `.npmrc`, and `.pypirc` are hidden from directory trees and blocked with `403`. Files over 1 MB or containing NUL/invalid UTF-8 bytes are rejected. | `internal/workspace/explorer.go` |
| **Audit logging** | Login attempts, pairing outcomes, origin mismatches, non-LAN rejections, sensitive file access attempts, prompt submissions, and tool approvals are logged as JSON lines to `~/.gravlink/audit.log`. | `internal/security/audit.go`, `internal/server/router.go` |

## Operational notes

- Avoid `--http` outside isolated debugging: without HTTPS, session cookies cannot set `Secure`, WebSocket upgrades may fail in installed PWAs, and credentials cross Wi-Fi unencrypted.
- Avoid `--allow-all-rfc1918` on shared or untrusted Wi-Fi networks unless you specifically need access from a secondary local subnet (such as a local VPN/tether bridge).
- Never share `http(s)://<ip>:8443/?pair=...` screenshots or logs while a token is active.
