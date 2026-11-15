# AGENTS.md

A Go + React web app: one Go binary serves a JSON API under `/api` and the embedded React SPA.
`CLAUDE.md` only imports this file; edit this one.

## Layout

```text
api/src/*.yaml          The wire contract, one file per domain; bundled into api/openapi.yaml, from
                        which the Go server types and the TS client are generated.
cmd/app/                main: `app serve` (default), `migrate`, `bootstrap-owner`, `add-adult`, `rekey`
internal/config/        The only package that reads the environment (lint-enforced)
internal/db/            Pool, goose migrations (migrations/*.sql), sqlc queries (queries/*.sql)
internal/db/dbq/        sqlc output. Generated: never edit.
internal/api/           oapi-codegen output (strict server interface + models). Generated: never edit.
internal/server/        Handlers (implement api.StrictServerInterface), routing, sessions
internal/httpx/         Middleware: security headers, client IP, rate limit, body limit, logs, recover
internal/auth/          Passwords (argon2id; old scrypt hashes rehashed on sign-in), tokens
internal/actor/         Who is calling: resolves cookies and bearer tokens to an Actor
internal/authz/         The authorization table, read from the spec's x-actors declarations
internal/command/       State changes: transaction, household lock, time after locks, idempotency
internal/ledger/        Points: award once per source, reverse, redeem, refund; never below zero
internal/clock/         The time: the database clock, or a Fake in tests
internal/day/           The household day: dates, times of day, windows, DST rules
internal/testdb/        A fresh migrated Postgres database per integration test
web/                    React + Vite app; web/src/api/schema.gen.ts is generated
web/design/             OpenCozy's design system (@app/design): tokens, components, previews (its README; why: DESIGN.md)
web/*.go                Go package that embeds web/dist (build tag embedweb) and serves it
charts/<app>/           Helm chart, published to oci://ghcr.io/oldsj/charts
tools/go.mod            Pinned Go tools (oapi-codegen, sqlc, golangci-lint, govulncheck, air)
```

## Rules

- **This repository is public.** Never commit secrets, real household data, real names,
  internal hostnames or addresses, or screenshots of a real installation. Fixtures and examples
  use fictional data and `example.test`. CI runs on GitHub-hosted runners only; never add
  self-hosted runners.

- **Contract first.** Change `api/src/*.yaml`, then `make gen`. Never edit `api/openapi.yaml`,
  `*.gen.go`, `internal/db/dbq/` or `web/src/api/schema.gen.ts`.
- **SQL only in `internal/db/queries/*.sql`**, through sqlc. Never build SQL from strings. Every
  query on household data takes the actor's household id (`WHERE household_id = $1 AND ...`).
- **Points only through `internal/ledger`**, under the child's lock; `point_ledger` is
  append-only. **Household days and hours only through `internal/day`.**
- **Schema changes are new goose migrations** (`internal/db/migrations/NNNNN_name.sql`); never edit
  one that has been merged.
- **The web app calls the API only through `web/src/api/client.ts`** (`$api` hooks, or `client`).
  ESLint forbids `fetch` and `dangerouslySetInnerHTML`.
- **Errors** are `{"error": {"code", "message"}}`: `code` is stable snake_case, `message` is safe
  to show. Return typed responses from handlers; return a Go `error` only for unexpected failures
  (logged, and the client gets a generic 500).
- **UI comes from the design system.** Build screens from `@app/design` components and token
  utilities (`bg-surface`, `text-muted-foreground`, `text-section`); never a hex colour, a Tailwind
  palette class (`stone-500`), `dark:` or an opacity modifier (`make design` fails on them).
  Something new goes into `web/design` first, with a preview: `web/design/README.md`. Product and
  design rules: `PRODUCT.md`, `DESIGN.md`.
- **No inline `<script>`, `<style>` or `style=` in the web app.** The CSP forbids them and the e2e
  test fails on any CSP violation. Use Tailwind classes.
- **Config** is env only, read in `internal/config`. Secrets have no defaults. Add new variables
  to `.env.example` and to the chart.
- Don't weaken security headers, CSP, cookie flags, rate limits or tests to make something pass.

## Add an endpoint, end to end

Example: let adults rename the household.

1. **Contract.** In the domain file `api/src/household.yaml` add the operation
   (`operationId: renameHousehold`) with limits on every field (`maxLength`, `minimum`,
   `additionalProperties: false`), a request `example`, and `$ref`s to `common.yaml`'s error
   responses. Declare who may call it: `x-actors: [adult]` (or `[public]`), plus `x-scopes` when
   service or assistant credentials are allowed, `x-display-scopes` for displays, `x-child-param`
   for a child in the path, `x-recent-auth: true` for sensitive changes, `x-body-limit` in bytes
   and `x-command: true` for any state change, with the required
   `$ref: common.yaml#/parameters/IdempotencyKey` header (add `x-record-response: false` when the
   response holds a secret). An operation without `x-actors` stops the server from starting
   (`internal/authz`). A new path also gets one line in `api/src/openapi.yaml`.
2. **Schema.** A new goose migration, `internal/db/migrations/NNNNN_name.sql`, if needed.
3. **Query.** In `internal/db/queries/households.sql`:
   `-- name: RenameHousehold :one` / `UPDATE household SET name = $2 WHERE id = $1 RETURNING *;`
   Every query on household data takes the household id from the actor, never from the request.
4. **Generate.** `make gen` bundles `api/src` into `api/openapi.yaml` and regenerates; the build
   fails until `*Server` implements `RenameHousehold`.
5. **Handler.** Take the actor with `mustActor(ctx)`. In an `x-command` operation use
   `mustCmd(ctx)`: its `Q` runs in the command's transaction with the household locked, lock
   children with `LockChildren` and take the time from `Now` (after the locks). Map
   `pgx.ErrNoRows` to the typed 404. Validation and authorization already happened.
6. **Test.** `TestAuthzMatrix` covers the new operation's access rules on its own. Add the flow
   to `internal/server/server_test.go` (real Postgres).
7. **UI.** Call `$api.useMutation('post', '/household/name', { onSuccess: invalidate })`. Add a
   Vitest test for non-trivial UI logic; extend `web/e2e/` only for a core flow.
8. **Check.** `make lint test`, then `make e2e`. Before a PR, `make ci`.

## Commands

```bash
make setup     # pnpm install + Playwright browser
make dev       # Postgres + Go API with hot reload (:8080) + Vite (http://localhost:5173)
make gen       # bundle api/src, regenerate from api/openapi.yaml and internal/db/
make fmt       # format Go, eslint --fix
make lint      # golangci-lint (incl. gosec), tsc, eslint, redocly
make design    # design system: build previews, check tokens, @dsCard markers, axe
make test      # go test -race against Postgres in Docker, vitest
make e2e       # build the real binary and run Playwright against it
make security  # govulncheck, pnpm audit (+ osv-scanner if installed)
make chart     # helm lint, kubeconform -strict, chart version-bump check
make ci        # all of the above, plus the multi-arch image build
```

Integration tests need `TEST_DATABASE_URL`; `make test` sets it to the Compose Postgres. Without
it they skip locally and fail in CI.

## Deploying

The chart in `charts/<app>/` is the deploy artifact. Any change under it needs a `version` bump
in `Chart.yaml` (`make chart` checks). A release sets `appVersion` to the image tag (the commit
SHA) and `image.digest` in the deployment's values file, which lives outside this repository.
Don't deploy from this repository.
