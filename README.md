# opencozy-next

A rewrite of OpenCozy, a self-hosted family dashboard, in Go and React. Work in progress: it does
not replace the current app yet. Plans are in [docs/rewrite](docs/rewrite).

## Stack

| | |
|-|-|
| API | Go 1.27, `net/http` ServeMux, `log/slog`, OpenAPI 3.1 contract with [oapi-codegen](https://github.com/oapi-codegen/oapi-codegen) strict server and request validation |
| Data | PostgreSQL 18, pgx v5, [sqlc](https://sqlc.dev), [goose](https://github.com/pressly/goose) migrations (embedded, run at start under an advisory lock) |
| Web | React 19, TypeScript 6 (strict), Vite 8, TanStack Query and Router, Tailwind 4, [openapi-fetch](https://openapi-ts.dev) client generated from the same contract, pnpm |
| Tests | Go unit + integration tests against real Postgres (`-race`), Vitest, one Playwright e2e of the whole flow against the production binary |
| Ship | One static binary embedding the SPA; distroless image for amd64 and arm64; Helm chart on `oci://ghcr.io/oldsj/charts` |
| Updates | Renovate, plus `renovate.json` |

```text
browser ──HTTPS──> gateway (Envoy) ──> app pod         (one Go binary, :8080)
                                                 ├── /healthz /readyz
                                                 ├── /api/*  security headers → CSRF check → body limit
                                                 │           → rate limit (auth) → session → OpenAPI validation
                                                 │           → strict handlers → sqlc → pgx ──TLS──> CNPG Postgres
                                                 └── /*      embedded SPA (hashed assets immutable, index no-cache)
```

Security defaults are in [SECURITY.md](SECURITY.md). How to work in the code, including adding an
endpoint end to end, is in [AGENTS.md](AGENTS.md).

## Develop

Needs Go, Node 26 with pnpm, Docker, and for `make chart` Helm and kubeconform.

```bash
make setup   # once
make dev     # Postgres in Docker, Go API with hot reload on :8080, Vite on http://localhost:5173
make ci      # everything CI runs
```

`make dev` copies `.env.example` to `.env` (local database). Create the first adult with
`go run ./cmd/app bootstrap-owner -household … -time-zone … -email … -name …` (password on stdin).

## Deploy

The Helm chart in `charts/opencozy-next/` is the deploy artifact, published as
`oci://ghcr.io/oldsj/charts/opencozy-next`. It holds the Deployment, Service, NetworkPolicy, a
CloudNativePG Cluster, and optionally a OnePasswordItem and the route (Certificate, ListenerSet
and HTTPRoute on a Gateway API gateway). `charts/opencozy-next/ci/example.yaml` shows a
deployment's values file; every value in it is a placeholder. Installation-specific values and
secrets live with the deployment, never in this repository.

After a deploy, `scripts/smoke.sh https://cozy.example.test` checks the installation without
changing anything: health, that the new server answers, the sign-in and error contract (including that the passkey domain fits the site, so imported passkeys sign in), the iOS
apps' passkey file, MCP `tools/list` and the router's gates (tokens from the environment; see the
script). It then prints a checklist for what only a person can check.

## Choices

Deliberate choices, inherited from the template this repository started from:

- **Go module at the repository root**, not `server/`, so `go:embed` can reach `web/dist` and
  the build is one binary. The SPA is embedded only with `-tags embedweb`; plain `go build` and
  `go test` work without a web build.
- **Tools in `tools/go.mod`** (`go tool -modfile=tools/go.mod ...`), so sqlc's and
  golangci-lint's dependencies stay out of the app's module graph and govulncheck's view, and
  Renovate still updates them.
- **No CORS, no Host/Origin guard of its own**: the SPA and API share an origin, and Go's
  `CrossOriginProtection` covers CSRF.
- **Postgres from Docker Compose locally and a service container in CI**, with the same image
  and credentials (`compose.yaml`); both run the same `make` targets against it.
- **Helm chart** rather than devspace/kustomize: a deployment consumes a versioned artifact.
- **pnpm `minimumReleaseAge`** (7 days) so a hand-added dependency gets the same cooldown as
  Renovate's.
- **TypeScript 6**, not 7: typescript-eslint and openapi-typescript don't support 7 yet.
- Postgres 18 for `uuidv7()` keys.

Not included, to stay lean: OpenTelemetry tracing and Prometheus metrics, email (verification,
password reset), MFA/passkeys, background jobs, i18n, a component library, devspace/kind,
preview environments, a dependency dashboard.

## License

OpenCozy is source-available under the [Business Source License 1.1](LICENSE). You may use,
modify and self-host it, including for your own household, school or organization. You may not
offer it to others as a hosted or managed service. Each version converts to the Apache License
2.0 four years after it is published.
