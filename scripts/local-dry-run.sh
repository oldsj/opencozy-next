#!/usr/bin/env bash
# A dry run of the import on your own machine, from a backup of the old app (runbook step 3).
#
#   scripts/local-dry-run.sh <dump> [<old-auth-secret-file>]
#
# <dump> is a pg_dump of the old app's database: custom format (-Fc), tar (-Ft), a directory
# (-Fd) or plain SQL, optionally gzipped (.gz). <old-auth-secret-file> holds the old app's
# BETTER_AUTH_SECRET (chmod 600); without it the Google, iCloud and Home Assistant secrets aren't
# opened and the report says so.
#
# It restores the dump into a throwaway Postgres container (the image compose.yaml pins, a random
# loopback port, its data in memory, removed on exit), builds the app, makes a fresh empty
# database next to it, and runs `app import --dry-run`: everything is imported, the report is
# printed, and the transaction is rolled back. Nothing is sent anywhere, and nothing is kept: the
# container, the database and the build go when the script ends. The report itself holds counts,
# not household data, but read it before you paste it anywhere.
#
# Passes when the report has no MISMATCH and nothing pending. Every line that isn't "ok" or "not
# carried" needs a look (docs/import.md, Reading the report).
#
# Needs Docker and Go. KEEP=1 leaves the container running for a look (remove it yourself).
# Works with macOS's bash 3.2.
set -euo pipefail

dump=${1:-}
secret=${2:-}
if [[ -z $dump || $dump == -h || $dump == --help ]]; then
  sed -n '2,21p' "$0" | sed 's/^# \{0,1\}//'
  exit 2
fi
[[ -e $dump ]] || { echo "local-dry-run: no such dump: $dump" >&2; exit 2; }
if [[ -n $secret ]]; then
  [[ -f $secret ]] || { echo "local-dry-run: no such secret file: $secret" >&2; exit 2; }
fi
for tool in docker go; do
  command -v "$tool" > /dev/null || { echo "local-dry-run: needs $tool" >&2; exit 2; }
done

root=$(cd "$(dirname "$0")/.." && pwd)
work=$(mktemp -d)
name="opencozy-local-dry-run-$$"
image=$(sed -n 's/^ *image: *\(postgres:.*\)$/\1/p' "$root/compose.yaml" | head -n 1)
[[ -n $image ]] || { echo "local-dry-run: no postgres image in compose.yaml" >&2; exit 1; }

cleanup() {
  if [[ ${KEEP:-} == 1 ]]; then
    echo "KEEP=1: container $name is still running; remove it with: docker rm -f $name"
  else
    docker rm -f "$name" > /dev/null 2>&1 || true
  fi
  rm -rf "$work"
}
trap cleanup EXIT
psql_in() { docker exec -i "$name" psql -X -q -v ON_ERROR_STOP=1 -U admin "$@"; }

echo "== Postgres ($image)"
# Trust authentication on a loopback port of a container that lives for this run only, with its
# data on a tmpfs: no passwords to keep, and the restored backup never reaches a disk.
docker run -d --rm --name "$name" -p 127.0.0.1::5432 --tmpfs /var/lib/postgresql:rw,size=4g \
  -e POSTGRES_USER=admin -e POSTGRES_DB=admin -e POSTGRES_HOST_AUTH_METHOD=trust -e POSTGRES_INITDB_ARGS=--no-sync \
  "$image" postgres -c fsync=off -c synchronous_commit=off -c full_page_writes=off > /dev/null
ready=0
for _ in $(seq 1 60); do
  # The image restarts the server once after init; wait for the final one.
  if docker exec "$name" pg_isready -q -U admin -h 127.0.0.1 2> /dev/null; then ready=1; break; fi
  sleep 1
done
[[ $ready == 1 ]] || { echo "local-dry-run: Postgres didn't start" >&2; exit 1; }
port=$(docker port "$name" 5432/tcp | head -n 1 | sed 's/.*://')
url() { echo "postgres://$1@127.0.0.1:$port/$2?sslmode=disable"; }

echo "== Build the app"
go build -C "$root" -o "$work/app" ./cmd/app

echo "== Restore $(basename "$dump")"
psql_in -c "CREATE DATABASE old"
start=$(date +%s)
if [[ -d $dump ]]; then
  docker cp "$dump" "$name:/tmp/dump" > /dev/null
  docker exec "$name" pg_restore --no-owner --no-acl --exit-on-error -U admin -d old /tmp/dump
  docker exec "$name" rm -rf /tmp/dump
else
  cat_dump() { if [[ $dump == *.gz ]]; then gzip -dc "$dump"; else cat "$dump"; fi; }
  magic=$(cat_dump 2> /dev/null | head -c 5 || true)
  tarmagic=$(cat_dump 2> /dev/null | head -c 262 | tail -c 5 || true)
  if [[ $magic == PGDMP ]]; then
    # Custom format.
    cat_dump | docker exec -i "$name" pg_restore --no-owner --no-acl --exit-on-error -U admin -d old
  elif [[ $tarmagic == ustar ]]; then
    cat_dump | docker exec -i "$name" pg_restore --no-owner --no-acl --exit-on-error -U admin -d old -Ft
  else
    # Plain SQL. The old app's roles don't exist here: drop ownership and grants, as
    # pg_restore --no-owner --no-acl does for the other formats.
    cat_dump | grep -v -E '^(ALTER [A-Z ]+ .* OWNER TO |GRANT |REVOKE |ALTER DEFAULT PRIVILEGES )' \
      | psql_in -d old > "$work/restore.txt" 2>&1 || { tail -n 20 "$work/restore.txt" >&2; exit 1; }
  fi
fi
echo "restored in $(($(date +%s) - start)) s"
# The importer reads the old app at migration 0045 (d7fead8), and refuses any other.
docker exec "$name" psql -X -At -U admin -d old -c \
  "SELECT 'old app: last migration ' || max(version) || ', ' || (SELECT count(*) FROM opencozy.point_ledger) || ' ledger rows'
   FROM opencozy.schema_migrations" 2> /dev/null || echo "old app: no opencozy.schema_migrations (is this the old app's database?)"

echo "== New database: owner cozy_owner migrates, cozy_runtime reads and writes rows"
psql_in -c "CREATE ROLE cozy_owner LOGIN" -c "CREATE ROLE cozy_runtime LOGIN" -c "CREATE DATABASE cozy OWNER cozy_owner"
DATABASE_URL=$(url cozy_runtime cozy)
MIGRATION_DATABASE_URL=$(url cozy_owner cozy)
# A key for this run only: the dry run encrypts the carried secrets, then rolls back.
OPENCOZY_SECRET_KEYS="dry-run:$(openssl rand -base64 32)"
export DATABASE_URL MIGRATION_DATABASE_URL DATABASE_RUNTIME_ROLE=cozy_runtime OPENCOZY_SECRET_KEYS LOG_LEVEL=warn

echo "== app import --dry-run"
args=(import --dry-run --source-url "$(url admin old)")
[[ -n $secret ]] && args+=(--old-auth-secret-file "$secret")
status=0
"$work/app" "${args[@]}" 2>&1 | tee "$work/report.txt" || status=$?

# Tables a report marks pending or MISMATCH: the first status word after the table name (ok,
# MISMATCH, pending, not carried), as in scripts/import-rehearsal.sh.
marked() {
  awk -v s="$1" '{ for (i = 2; i <= NF; i++) if ($i ~ /^(ok|MISMATCH|pending|not)$/) { if ($i == s) print $1; break } }' "$work/report.txt" | sort -u
}
echo
echo "== Summary"
mismatch=$(marked MISMATCH)
pending=$(marked pending)
[[ -z $mismatch ]] || echo "MISMATCH: $(echo "$mismatch" | tr '\n' ' ')"
[[ -z $pending ]] || echo "pending (a real import refuses until these are mapped): $(echo "$pending" | tr '\n' ' ')"
[[ -n $secret ]] || echo "No old auth secret given: Google, iCloud and Home Assistant secrets weren't opened."
if [[ $status != 0 || -n $mismatch || -n $pending ]]; then
  echo "Dry run FAILED (exit $status). Send the coordinator the lines above, not the dump."
  exit 1
fi
echo "Dry run passed: no MISMATCH, nothing pending. Read every line that isn't ok or not carried."
