# Deploying OpenCozy

Every setting a deployment needs, in one place: the server's environment, the Helm chart's
values, the outside systems that talk to OpenCozy (router, Home Assistant, Google, Apple) and the
apps. Every value here is a placeholder (`example.test`, `ABCDE12345`). Real values live in the
deployment's own values file and its Secret, outside this repository.

The chart is `charts/opencozy-next`, published to `oci://ghcr.io/oldsj/charts`. A values file to
start from: `charts/opencozy-next/ci/example.yaml`.

## Server environment

The server reads only its environment (`internal/config`). It refuses to start on an invalid
value, and secrets have no defaults. In the chart, plain values come from `config.*` and secrets
from `secrets.env` (below).

| Variable | Chart value | Default | What |
|---|---|---|---|
| `DATABASE_URL` | `database.*` | none | Postgres URL. With `database.cnpg.enabled` the chart wires CloudNativePG's generated Secret. |
| `MIGRATION_DATABASE_URL` | `database.*` | `DATABASE_URL` | The schema owner, used by the migrate init container. |
| `DATABASE_RUNTIME_ROLE` | `database.runtime.role` | none | The role the app serves as (rows only, granted by migrations), from `database.runtime.existingSecret`. |
| `MIGRATE_ON_START` | set to `false` | `true` | The chart runs migrations in an init container instead. |
| `LISTEN_ADDR` | set by the chart | `:8080` | |
| `LOG_LEVEL` | `config.logLevel` | `info` | `debug`, `info`, `warn`, `error`. |
| `SESSION_TTL` | `config.sessionTTL` | `720h` | Adult web sessions, 1m to 2160h. |
| `AUTH_RATE_PER_MINUTE` | `config.authRatePerMinute` | `10` | Sign-in attempts per client per minute, per pod. |
| `TRUSTED_PROXIES` | `config.trustedProxies` | none | CIDRs (comma-separated) whose `X-Forwarded-For` is trusted: the gateway's pod network, e.g. `10.0.0.0/16`. |
| `PUBLIC_ORIGIN` | `config.publicOrigin` | none | The origin people use, `https://opencozy.example.test`. Kid devices are told it at pairing; OIDC and Google return to it. Without it, devices pair only on localhost; the chart requires it with OIDC. |
| `WEBAUTHN_RP_ID` | `config.webauthnRPID` | off | Passkeys' relying party ID: the site's hostname, `opencozy.example.test`. **Never change it once passkeys exist** (see [Cutover](#cutover-from-the-old-app)). |
| `WEBAUTHN_ORIGINS` | `config.webauthnOrigins` | none | Comma-separated origins passkeys are used from, `https://opencozy.example.test`. Required with the RP ID. |
| `RECENT_AUTH_WINDOW` | not in the chart | `10m` | How recent a sign-in must be for sensitive changes. Tests only. |
| `OPENCOZY_SECRET_KEYS` | `secrets.env` | none | **Secret.** Keyring for integration secrets at rest: `id:base64key,…`, 32 random bytes each, the first encrypts. Make one: `printf 'k1:%s\n' "$(openssl rand -base64 32)"`. Rotate with `app rekey`. |
| `HOME_ASSISTANT_ALLOWED_PRIVATE_CIDRS` | `egress.homeAssistant.cidrs` | none | Private ranges (RFC 1918 or ULA only) Home Assistant may be reached on. |
| `OIDC_ISSUER` | `config.oidc.issuer` | off | Single sign-on for adults: the provider's issuer, https, exactly as its discovery document says, `https://id.example.test`. |
| `OIDC_CLIENT_ID` | `config.oidc.clientId` | none | OpenCozy's client at the provider. |
| `OIDC_CLIENT_SECRET` | `secrets.env` | none | **Secret.** The client's secret. |
| `OIDC_NAME` | `config.oidc.name` | `single sign-on` | Button text after "Sign in with", e.g. `Example ID`. |
| `OIDC_ALLOWED_PRIVATE_CIDRS` | `egress.oidc.cidrs` | none | Private ranges a self-hosted provider may be reached on. |
| `GOOGLE_CLIENT_ID` | `config.googleClientID` | off | Google calendars: an OAuth client ID, `000000000000-example.apps.googleusercontent.com`. |
| `GOOGLE_CLIENT_SECRET` | `secrets.env` | none | **Secret.** |
| `APPLE_TEAM_ID` | `config.appleTeamID` | off | Serves `/.well-known/apple-app-site-association`, so the iOS app can use this site's passkeys: `ABCDE12345`. |
| `APPLE_BUNDLE_ID` | `config.appleBundleID` | none | The iOS app's bundle ID, `org.example.opencozy`. Required with the team ID. |
| `APNS_KEY_ID` | `config.apnsKeyID` | off | APNs auth key ID, `KEY1234567`. With `APNS_KEY`, pushes to parent phones and kid devices. |
| `APNS_KEY` | `secrets.env` | none | **Secret.** The `.p8` key's PEM. Checked at startup, never logged. |

## Chart values

Beyond the `config.*` values above (defaults in `charts/opencozy-next/values.yaml`):

- **`image.digest`** (required): the multi-arch image digest for the chart's `appVersion` (the
  commit SHA). Images are never pulled by tag.
- **`replicaCount`** `2`, **`spread`** (pods on different nodes) and **`podDisruptionBudget`**
  (`minAvailable: 1`).
- **`database.cnpg`**: `enabled` (default), `instances`, `imageName`, `storage.size`,
  `storage.storageClass`, `resources`. Or, with cnpg off: `database.existingSecret`, `urlKey`,
  `caSecret` (a Secret with `ca.crt`, for `verify-full`) and `networkPolicy.databaseTo`.
- **`secrets`**: the app's secrets as env vars from one Secret, either `secrets.existingSecret` or
  the OnePasswordItem the chart makes from `onepassword.itemPath`
  (`vaults/<vault>/items/<item>`). Secret values never go in values:
  ```yaml
  secrets:
    env:
      - name: OPENCOZY_SECRET_KEYS
        key: secret-keys
      - name: OIDC_CLIENT_SECRET
        key: oidc-client-secret
      - name: GOOGLE_CLIENT_SECRET
        key: google-client-secret
      - name: APNS_KEY
        key: apns-key
  onepassword:
    itemPath: vaults/example/items/opencozy
  ```
- **`route`**: `enabled`, `hostname` (`opencozy.example.test`), `gateway.name`,
  `gateway.namespace`, `clusterIssuer`. Makes a cert-manager Certificate, a ListenerSet and an
  HTTPRoute.
- **`networkPolicy`**: `enabled` (default), `ingressFrom` (match it to the gateway), `dnsTo`,
  `databaseTo`, `databasePort`, `extraEgress` (appended as-is; use a selector for anything inside
  the cluster, because ipBlock doesn't match Service IPs on most CNIs).
- **`egress`**: what the app may reach outside the cluster. The app checks every address itself
  (`internal/egress`); this adds the matching NetworkPolicy.
  - `internet: false` by default. Set `true` for anything public: a public OIDC provider, Google,
    APNs at `api.push.apple.com`, ICS calendar feeds. The chart refuses an OIDC issuer
    with network policy on and neither `internet` nor `oidc.cidrs`.
  - `homeAssistant.cidrs` / `ports` (`[8123]`); `oidc.cidrs` / `ports` (`[443]`).
- **`resources`**: requests and limits for the app container.
- Any change under `charts/` needs a `version` bump in `Chart.yaml` (`make chart` checks).

## Outside systems

### Single sign-on (OIDC)
Register `PUBLIC_ORIGIN/api/auth/callback/oidc`, e.g.
`https://opencozy.example.test/api/auth/callback/oidc`, as the client's redirect URI. OpenCozy asks for
`openid email profile`. Adults are linked by their verified email at first sign-in; nothing is imported.

### Google calendars
- An OAuth client of type "Web application" in Google Cloud, with redirect URI
  `PUBLIC_ORIGIN/api/calendar/google/callback`. Scopes: `openid email` and
  `https://www.googleapis.com/auth/calendar.readonly`. Nothing writes to Google.
- Needs `PUBLIC_ORIGIN`, `OPENCOZY_SECRET_KEYS` and `egress.internet: true`. The app reaches only
  `oauth2.googleapis.com`, `openidconnect.googleapis.com` and `www.googleapis.com`.
- **After an import from the old app, reuse the old app's OAuth client** (its
  `GOOGLE_CALENDAR_CLIENT_ID` and secret) and add the redirect URI above to it. Imported refresh
  tokens only work with the client that issued them; with a new client every imported account
  shows "Sign in again". The old tokens are carried only with `app import --old-auth-secret-file`.

### Router (TV gate)
The router script authenticates with a Service key with scope `tv:gate` (Manage → Connections):
`Authorization: Bearer ocz_…`.

| Call | What |
|---|---|
| `GET /api/tv/allowances` | → `{serverTime, allowances: [{mac, secondsRemaining}]}`. Each allowance is at most 300 s, so a router that loses the server closes TVs within 5 minutes. Poll every few seconds. |
| `POST /api/tv/attempts` | `{attempts: [{mac, lastSeenAt}]}`, at most 50, at most once a minute per MAC. Needs a **new `Idempotency-Key` header on every report** (1–128 printable characters). |

### Home Assistant (TV power)
Home Assistant pushes power states with a Service key with scope `tv:report`; OpenCozy does not
poll it. Send the media player's own state (`playing`, `paused`, `idle`, `off`, …), with a new
`Idempotency-Key` each time, and resend every 5 minutes: a TV idle or paused for 15 minutes counts
as unknown, judged when a report arrives.

```yaml
rest_command:
  opencozy_tv_power:
    url: https://opencozy.example.test/api/tv/power
    method: post
    headers:
      Authorization: !secret opencozy_tv_report   # "Bearer ocz_..."
      Idempotency-Key: "{{ entity_id }}-{{ now().timestamp() }}"
    content_type: application/json
    payload: '{"entityId":"{{ entity_id }}","state":"{{ states(entity_id) }}","lastChanged":"{{ states[entity_id].last_changed.isoformat() }}"}'
automation:
  - alias: OpenCozy TV power
    mode: queued
    trigger:
      - platform: state
        entity_id: [media_player.example_tv]
      - platform: time_pattern
        minutes: /5
    action:
      - repeat:
          for_each: [media_player.example_tv]
          sequence:
            - service: rest_command.opencozy_tv_power
              data: { entity_id: "{{ repeat.item }}" }
```

Each TV's Home Assistant entity is set on the TVs page (Edit).

### Apple: passkeys in the app and pushes
- `APPLE_TEAM_ID` and `APPLE_BUNDLE_ID` must match the iOS build's team and bundle ID
  (`ios/Config/Local.xcconfig`), and the app's associated domain is `OPENCOZY_WEB_HOST` = the
  passkey RP ID's host.
- An APNs auth key (Certificates, Identifiers & Profiles → Keys, with APNs): its key ID as
  `APNS_KEY_ID`, the `.p8` file's contents as the `APNS_KEY` secret. Needs `egress.internet: true`.

## Apps

### iOS
Signing values live in `ios/Config/Local.xcconfig` (gitignored): `DEVELOPMENT_TEAM`,
`OPENCOZY_BUNDLE_ID`, `OPENCOZY_APP_GROUP`, `OPENCOZY_WEB_HOST`. TestFlight steps, and the Family
Controls distribution entitlement they need: `ios/README.md#testflight`. A kid iPad pairs from
Manage → Devices; the server it pairs with is `PUBLIC_ORIGIN`.

### Android wall display
No server address is built in: on first start the display asks for the server's origin
(`https://opencozy.example.test`) and opens `/display`, which pairs with a code. Application ID
`org.opencozy.display`. Release builds take their signing from the environment only:
`OPENCOZY_RELEASE_KEYSTORE`, `OPENCOZY_RELEASE_KEY_ALIAS`, `OPENCOZY_RELEASE_STORE_PASSWORD`,
`OPENCOZY_RELEASE_KEY_PASSWORD`. `OPENCOZY_ORIGIN` (optional) only pre-fills the address field.
Details: `android/README.md`.

## Cutover from the old app

What changes for things outside this server when a household moves from the old app:

- **Passkeys:** keep the same hostname. `WEBAUTHN_RP_ID` must be the old app's hostname and
  `WEBAUTHN_ORIGINS` must include the old origin; then imported passkeys sign in as before. If the
  hostname changes, adults sign in with their password and add a passkey again.
- **Router and Home Assistant:** the old `/api/v1/tv/allowances`, `/api/v1/tv/attempts` and
  `/api/v1/tv/events` become the calls above (`events` → `power`, with HA's own state and an
  `Idempotency-Key`). Until the router script changes, it gets no allowances, so every gated TV
  stays offline (fail closed). Imported Service keys keep their scopes. Imported TVs start with
  power unknown.
- **Google:** reuse the old OAuth client (above).
- **Every `/api/v1/` path answers 410 `update_app`**, so an old app says it is out of date.
- **iPads:** install the new build, then re-pair from Manage → Devices if the device asks.
- **Android display:** it is a new application ID, so install it next to the old app,
  pair it, then remove the old one.
