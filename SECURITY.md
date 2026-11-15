# Security

## Reporting

Report vulnerabilities privately through GitHub's **Security → Report a vulnerability** on this
repository, not in a public issue.

## Defaults

What every project made from this template starts with, and where it lives.

| Area | Default | Where |
|------|---------|-------|
| Sessions | Random 256-bit token in `__Host-session` (HttpOnly, Secure, SameSite=Lax, Path=/); only its SHA-256 is stored, in Postgres; expires after `SESSION_TTL` (30 days); a new token on every sign-in; sign-out deletes it | `internal/server/session.go` |
| Passwords | argon2id (19 MiB, t=2), 12–128 characters; sign-in takes the same time whether or not the account exists | `internal/auth` |
| Sign-up | Off unless `ALLOW_SIGNUP=true` | `internal/config` |
| CSRF | Go's `http.CrossOriginProtection` refuses cross-origin unsafe requests (`Sec-Fetch-Site`/`Origin`), plus SameSite=Lax | `internal/server/server.go` |
| Brute force | Token bucket per client address on sign-in and password confirmation (`AUTH_RATE_PER_MINUTE`, 10); `X-Forwarded-For` believed only from `TRUSTED_PROXIES` | `internal/httpx` |
| Browser | CSP `default-src 'self'`, no inline script or style, Trusted Types, `frame-ancestors 'none'`; HSTS, nosniff, `X-Frame-Options: DENY`, COOP/CORP same-origin, Referrer-Policy, Permissions-Policy | `internal/httpx/headers.go` |
| Input | Every request is validated against `api/openapi.yaml` (types, formats, lengths, no unknown fields) before a handler runs; 1 MiB body limit | `internal/server/server.go` |
| Server | Read-header 5 s, read 15 s, write 30 s, idle 60 s timeouts; 64 KiB headers; panics become a logged 500 | `internal/server/serve.go` |
| Errors and logs | Clients get a code and a safe message, never internals; logs never include headers, cookies or bodies | `internal/httpx` |
| SQL | sqlc only: parameterized queries, owner checked in every query | `internal/db/queries` |
| Database | TLS required; on the cluster, certificate and hostname verified (`verify-full` against the CNPG CA); `sslmode=disable` refused for anything but localhost | `internal/config`, chart |
| Secrets | Env only, no working defaults; Kubernetes Secrets from 1Password; `.env` is ignored by Git | `.env.example`, chart |
| Container | Distroless static, uid 65532, read-only root, all capabilities dropped, no privilege escalation, seccomp RuntimeDefault, no service account token | `Dockerfile`, chart |
| Network | NetworkPolicy: ingress only from the gateway's proxies, egress to DNS and the database; `egress.internet` adds public addresses on 80, 443, 8080 and 8443, and each `egress.*.cidrs` list adds that private range on its ports. `internal/egress` also checks every outbound address in the app | chart, `internal/egress` |
| Supply chain | Actions pinned by SHA with least `permissions`; images pinned by digest; pnpm refuses packages under 7 days old and runs no install scripts; Renovate waits 7 days too; govulncheck, gosec, pnpm audit, osv-scanner, CodeQL; image provenance and SBOM | `.github/`, `web/pnpm-workspace.yaml` |

## Known limits

- Rate limits are per replica, in memory. Behind several replicas, the effective limit multiplies.
- No account lockout, email verification, password reset or MFA. There is no sign-up: adults are added with `app bootstrap-owner` and `app add-adult`. Add them before opening sign-up
  to strangers.
- No audit log of sign-ins.
