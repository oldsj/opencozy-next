#!/usr/bin/env bash
# Cutover smoke test (runbook step 6): read-only checks against a running OpenCozy, then a
# checklist for what only a person can check.
#
#   scripts/smoke.sh https://cozy.example.test
#
# Tokens come from the environment, never the command line (it shows in ps and shell history):
#   OPENCOZY_SMOKE_MCP_TOKEN     an assistant credential's secret (POST /api/mcp tools/list)
#   OPENCOZY_SMOKE_ROUTER_TOKEN  the router's service credential (tv:gate and games:gate)
# Without one, its checks are skipped and the run fails, unless SMOKE_ALLOW_SKIP=1.
#   SMOKE_TV_MAC                 the MAC asked about (default 00:00:5e:00:53:01, from the range
#                                RFC 7042 keeps for documentation, so no TV has it)
#   SMOKE_REQUIRE_APPLE=0        don't require /.well-known/apple-app-site-association
# A private CA: CURL_CA_BUNDLE=/path/to/ca.pem.
#
# Every request is a GET, except tools/list, which reads. Nothing is changed on the server.
set -euo pipefail

base=${1:-}
if [[ -z $base || $base == -h || $base == --help ]]; then
  sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'
  exit 2
fi
base=${base%/}
for tool in curl jq; do
  command -v "$tool" > /dev/null || { echo "smoke: needs $tool" >&2; exit 2; }
done

mac=${SMOKE_TV_MAC:-00:00:5e:00:53:01}
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
passed=0 failed=0 skipped=0

pass() { passed=$((passed + 1)); printf '  ok    %s\n' "$1"; }
fail() { failed=$((failed + 1)); printf '  FAIL  %s\n' "$1"; }
skip() { skipped=$((skipped + 1)); printf '  skip  %s\n' "$1"; }

# req <method> <path> [curl args...]: status in $status, body in $work/body, headers in
# $work/headers. A connection failure is status 000.
req() {
  local method=$1 path=$2
  shift 2
  status=$(curl -sS -o "$work/body" -D "$work/headers" -w '%{http_code}' --max-time 15 \
    -X "$method" "$@" "$base$path" 2> "$work/err") || status=000
}
header() { grep -i "^$1:" "$work/headers" | head -1 | cut -d: -f2- | tr -d '\r' | sed 's/^ //'; }
body() { head -c 300 "$work/body" | tr -d '\n'; }
# bearer <token>: a curl header file, so the token never appears in a process listing.
bearer() { printf 'Authorization: Bearer %s\n' "$1" > "$work/auth"; chmod 600 "$work/auth"; }

echo "OpenCozy smoke test: $base"

echo "== Health"
req GET /healthz
if [[ $status == 000 ]]; then
  fail "GET /healthz: no answer ($(tr -d '\n' < "$work/err"))"
  echo "Nothing answers at $base; stopping." >&2
  exit 1
fi
if [[ $status == 204 ]]; then pass "GET /healthz 204"; else fail "GET /healthz $status, want 204"; fi
req GET /readyz
if [[ $status == 204 ]]; then pass "GET /readyz 204 (database reachable, not draining)"
else fail "GET /readyz $status, want 204: $(body)"; fi

echo "== This is the new server"
# The old app served /api/v1; the new one tells its apps to update.
req GET /api/v1/children
if [[ $status == 410 ]] && jq -e '.error.code == "update_app"' "$work/body" > /dev/null 2>&1; then
  pass "GET /api/v1/children 410 update_app (old apps are told to update)"
else
  fail "GET /api/v1/children $status, want 410 update_app: is the route still on the old app? $(body)"
fi
req GET /
if [[ $status == 200 && $(header content-type) == text/html* ]]; then
  pass "GET / serves the web app"
else
  fail "GET / $status $(header content-type), want 200 text/html"
fi
for h in content-security-policy strict-transport-security x-content-type-options x-frame-options; do
  if [[ -n $(header "$h") ]]; then pass "header $h"; else fail "header $h missing on /"; fi
done

echo "== Discovery and contract"
req GET /api/auth/methods
if [[ $status == 200 ]] && jq -e '(.passkeys | type) == "boolean" and has("oidc")' "$work/body" > /dev/null 2>&1; then
  pass "GET /api/auth/methods: passkeys $(jq -r .passkeys "$work/body"), single sign-on $(jq -r '.oidc.name // "off"' "$work/body")"
  # Imported passkeys sign in only on the name they were made for: WEBAUTHN_RP_ID must be this
  # site's host (or a parent domain of it), and WEBAUTHN_ORIGINS must list this origin.
  if [[ $(jq -r .passkeys "$work/body") != true ]]; then
    fail "passkeys are off: imported passkeys can't sign in (WEBAUTHN_RP_ID)"
  else
    [[ $base =~ ^(https?://[^/]+) ]] && origin=$(tr '[:upper:]' '[:lower:]' <<< "${BASH_REMATCH[1]}")
    host=${origin#*://} host=${host%%:*}
    rp=$(jq -r '.passkeyRpId // empty' "$work/body")
    if [[ -z $rp ]]; then
      fail "the server doesn't say its passkey domain (passkeyRpId): can't check imported passkeys"
    elif [[ $host == "$rp" || $host == *."$rp" ]]; then
      pass "passkey domain $rp fits $host"
    else
      fail "passkey domain $rp isn't $host: imported passkeys can't sign in (WEBAUTHN_RP_ID)"
    fi
    if jq -e --arg o "$origin" '(.passkeyOrigins // []) | map(ascii_downcase) | index($o) != null' "$work/body" > /dev/null; then
      pass "passkey origins include $origin"
    else
      fail "passkey origins $(jq -c '.passkeyOrigins // []' "$work/body") don't include $origin (WEBAUTHN_ORIGINS)"
    fi
  fi
else
  fail "GET /api/auth/methods $status: $(body)"
fi
req GET /api/auth/me
if [[ $status == 401 ]] && jq -e '(.error.code | type) == "string" and (.error.message | type) == "string"' "$work/body" > /dev/null 2>&1; then
  pass "GET /api/auth/me signed out: 401 $(jq -r .error.code "$work/body") (the error shape)"
else
  fail "GET /api/auth/me signed out $status, want 401 {error: {code, message}}: $(body)"
fi
req GET /.well-known/apple-app-site-association
if [[ $status == 200 ]] && jq -e '.webcredentials.apps | length > 0' "$work/body" > /dev/null 2>&1; then
  pass "apple-app-site-association: $(jq -r '.webcredentials.apps | join(", ")' "$work/body") (passkeys in the iOS apps)"
elif [[ ${SMOKE_REQUIRE_APPLE:-1} == 0 ]]; then
  skip "apple-app-site-association $status (SMOKE_REQUIRE_APPLE=0)"
else
  fail "apple-app-site-association $status $(header content-type), want 200 JSON: APPLE_TEAM_ID and APPLE_BUNDLE_ID set?"
fi

echo "== MCP"
if [[ -z ${OPENCOZY_SMOKE_MCP_TOKEN:-} ]]; then
  skip "tools/list: OPENCOZY_SMOKE_MCP_TOKEN not set"
else
  bearer "$OPENCOZY_SMOKE_MCP_TOKEN"
  req POST /api/mcp -H @"$work/auth" -H 'Content-Type: application/json' \
    -H 'Accept: application/json, text/event-stream' \
    --data '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'
  if [[ $status == 200 ]] && jq -e '.result.tools | length > 0' "$work/body" > /dev/null 2>&1; then
    pass "tools/list: $(jq '.result.tools | length' "$work/body") tools ($(jq -r '[.result.tools[].name] | .[:5] | join(", ")' "$work/body"), ...)"
  else
    fail "POST /api/mcp tools/list $status: $(body)"
  fi
fi

echo "== Router gate"
if [[ -z ${OPENCOZY_SMOKE_ROUTER_TOKEN:-} ]]; then
  skip "TV and games allowances: OPENCOZY_SMOKE_ROUTER_TOKEN not set"
else
  bearer "$OPENCOZY_SMOKE_ROUTER_TOKEN"
  req GET /api/tv/allowances -H @"$work/auth"
  if [[ $status == 200 ]] && jq -e '(.allowances | type) == "array"' "$work/body" > /dev/null 2>&1; then
    left=$(jq -r --arg m "$mac" '[.allowances[] | select((.mac | ascii_downcase) == ($m | ascii_downcase)) | .secondsRemaining][0] // empty' "$work/body")
    if [[ -n $left ]]; then
      if [[ -n ${SMOKE_TV_MAC:-} ]]; then pass "TV gate: $mac may be online for ${left}s"
      else fail "TV gate: the documentation MAC $mac is allowed online (${left}s)"; fi
    else
      pass "TV gate answers: $mac stays offline ($(jq '.allowances | length' "$work/body") MACs online now)"
    fi
  else
    fail "GET /api/tv/allowances $status: $(body)$([[ $status == 403 ]] && echo ' (the token needs tv:gate)')"
  fi
  req GET /api/games/allowances -H @"$work/auth"
  if [[ $status == 200 ]] && jq -e '(.allowances | type) == "array"' "$work/body" > /dev/null 2>&1; then
    pass "games gate answers: $(jq '.allowances | length' "$work/body") children with iPad time running"
  else
    fail "GET /api/games/allowances $status: $(body)$([[ $status == 403 ]] && echo ' (the token needs games:gate)')"
  fi
fi

cat <<EOF

== By hand (the runbook's go / no-go)
  [ ] Adult sign-in: sign in at $base with an imported passkey (and single sign-on if it is on);
      Today shows the household's children and points.
  [ ] Wall display: the wall shows Today (re-pair it if it shows the pairing code), and checking
      a chore there reaches the adult's inbox.
  [ ] One iPad: it syncs (allowed hours and banked time look right); start a little banked
      time and see games open on the router, then end it.
  [ ] One TV: turn it on; the "Who's watching?" prompt appears, and the TV goes online when
      answered.
  [ ] One parent phone: sign in, approve something from the inbox, and receive a push (ask for
      time on the iPad).
  [ ] Assistant: the assistant's MCP client lists the tools and reads the balances.
EOF

echo
echo "$passed passed, $failed failed, $skipped skipped"
if ((failed > 0)); then
  echo "Smoke test FAILED: no-go until fixed (or roll back)."
  exit 1
fi
if ((skipped > 0)) && [[ ${SMOKE_ALLOW_SKIP:-0} != 1 ]]; then
  echo "Smoke test incomplete: set the tokens above (or SMOKE_ALLOW_SKIP=1)."
  exit 1
fi
echo "Smoke test passed. Now the checklist above."
