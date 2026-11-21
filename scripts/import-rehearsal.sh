#!/usr/bin/env bash
# Import rehearsal (cutover runbook prerequisite 3), with fictional data only.
#
#   scripts/import-rehearsal.sh            # or: make import-rehearsal
#   ALLOW_PENDING=1 scripts/import-rehearsal.sh
#
# 1. Starts a throwaway Postgres (Docker, the image compose.yaml pins, a random local port, removed
#    on exit) and loads the old app's schema frozen at d7fead8 (migration 0045), as vendored for the
#    importer's tests (internal/importer/testdata/old_schema.sql).
# 2. Seeds a fictional household: the importer's fixture (testdata/old_seed.sql), then
#    import-rehearsal/seed.sql (a year of a daily chore, approved and paid; open sessions and other rows
#    that aren't carried), then import-rehearsal/pending.sql (tables the importer can't map yet).
# 3. Runs `app import --dry-run` twice (the two reports must be the same), then `app import` into a
#    fresh database, migrated by its owner as in production, with a row-only runtime role.
#    Independently of the importer's own checks, each child's points and banked iPad and TV
#    seconds must match.
# 4. Runs `app import` again: it must refuse the non-empty target and change nothing.
#
# Any MISMATCH, a failed check, a report that differs between dry runs, or a second run that
# writes anything fails the rehearsal. So does a pending table, because a real import refuses
# while one is pending. ALLOW_PENDING=1 leaves pending.sql out and applies the importer's
# testdata/complete.sql (which removes the fixture's pending parts), so the real import and the
# second run are rehearsed today. The rehearsal is only complete without it.
#
# Needs Docker and Go. Prints timings. DAYS=3650 seeds ten years of the daily chore instead of one
# (more volume for the timing). KEEP=1 leaves the container running for a look.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
here="$root/scripts/import-rehearsal"
testdata="$root/internal/importer/testdata"
work=$(mktemp -d)
name="opencozy-import-rehearsal-$$"
image=$(sed -n 's/^ *image: *\(postgres:.*\)$/\1/p' "$root/compose.yaml" | head -n 1)
[ -n "$image" ] || { echo "no postgres image in compose.yaml" >&2; exit 1; }

cleanup() {
  if [ "${KEEP:-}" = 1 ]; then
    echo "KEEP=1: container $name is still running; remove it with: docker rm -f $name"
  else
    docker rm -f "$name" > /dev/null 2>&1 || true
  fi
  rm -rf "$work"
}
trap cleanup EXIT

now() { if [ -n "${EPOCHREALTIME:-}" ]; then echo "${EPOCHREALTIME/,/.}"; else date +%s; fi; }
timings=()
timed() { # timed <label> <command...>: runs it, records how long it took, keeps its status
  local label=$1 start status=0
  shift
  start=$(now)
  "$@" || status=$?
  timings+=("$(awk -v a="$start" -v b="$(now)" -v l="$label" 'BEGIN { printf "%-34s %8.2f s", l, b - a }')")
  return "$status"
}
failures=()
fail() { echo "FAIL: $*"; failures+=("$*"); }
psql_in() { docker exec -i "$name" psql -X -q -v ON_ERROR_STOP=1 -U admin "$@"; }

echo "== Postgres ($image)"
# Trust authentication on a loopback port of a container that lives for this run only: no
# passwords to keep anywhere.
docker run -d --rm --name "$name" -p 127.0.0.1::5432 \
  -e POSTGRES_USER=admin -e POSTGRES_DB=admin -e POSTGRES_HOST_AUTH_METHOD=trust -e POSTGRES_INITDB_ARGS=--no-sync \
  "$image" postgres -c fsync=off -c synchronous_commit=off -c full_page_writes=off > /dev/null
for _ in $(seq 1 60); do
  # The image restarts the server once after init; wait for the final one.
  docker exec "$name" pg_isready -q -U admin -h 127.0.0.1 2> /dev/null && break
  sleep 1
done
port=$(docker port "$name" 5432/tcp | head -n 1 | sed 's/.*://')
base="postgres://%s@127.0.0.1:$port/%s?sslmode=disable"

echo "== Build"
timed "build app" go build -C "$root" -o "$work/app" ./cmd/app

echo "== Old database (d7fead8, 0045) with the fictional household"
seed() {
  psql_in -c "CREATE DATABASE old"
  psql_in -d old < "$testdata/old_schema.sql" > /dev/null
  psql_in -d old < "$testdata/old_seed.sql"
  psql_in -d old -v days="${DAYS:-373}" < "$here/seed.sql"
  if [ "${ALLOW_PENDING:-}" = 1 ]; then
    psql_in -d old < "$testdata/complete.sql"
  else
    psql_in -d old < "$here/pending.sql"
  fi
}
timed "load old schema and seed" seed
psql_in -d old -At -c "SELECT 'old rows: ' || (SELECT count(*) FROM opencozy.point_ledger) || ' ledger, '
  || (SELECT count(*) FROM opencozy.chore_occurrence) || ' chore occurrences, '
  || (SELECT count(*) FROM opencozy.completion_approval) || ' approvals'"

echo "== New database: owner cozy_owner migrates, cozy_runtime reads and writes rows"
psql_in -c "CREATE ROLE cozy_owner LOGIN" -c "CREATE ROLE cozy_runtime LOGIN" -c "CREATE DATABASE cozy OWNER cozy_owner"
export DATABASE_URL MIGRATION_DATABASE_URL DATABASE_RUNTIME_ROLE=cozy_runtime OPENCOZY_SECRET_KEYS LOG_LEVEL=warn
# shellcheck disable=SC2059
DATABASE_URL=$(printf "$base" cozy_runtime cozy)
# shellcheck disable=SC2059
MIGRATION_DATABASE_URL=$(printf "$base" cozy_owner cozy)
OPENCOZY_SECRET_KEYS="rehearsal:$(openssl rand -base64 32)"
# shellcheck disable=SC2059
source_url=$(printf "$base" admin old)

app_import() { "$work/app" import --source-url "$source_url" "$@"; }

# Tables a report marks pending or MISMATCH: the first column of a line whose status says so. The
# status is the first status word after the table name (ok, MISMATCH, pending, not carried); words
# in the notes after it don't count.
marked() {
  awk -v s="$1" '{ for (i = 2; i <= NF; i++) if ($i ~ /^(ok|MISMATCH|pending|not)$/) { if ($i == s) print $1; break } }' "$2" | sort -u
}

echo "== Dry run (twice)"
for n in 1 2; do
  status=0
  timed "dry run $n" app_import --dry-run > "$work/dry$n.txt" 2>&1 || status=$?
  [ "$status" = 0 ] || fail "dry run $n exited $status: $(tail -n 1 "$work/dry$n.txt")"
done
cat "$work/dry1.txt"
diff -u "$work/dry1.txt" "$work/dry2.txt" > "$work/dry.diff" || fail "the two dry runs' reports differ: $(cat "$work/dry.diff")"
mismatched=$(marked MISMATCH "$work/dry1.txt")
[ -z "$mismatched" ] || fail "mismatches: $(echo "$mismatched" | tr '\n' ' ')"
pending=$(marked pending "$work/dry1.txt")
if [ -n "$pending" ]; then
  fail "pending (a real import refuses until the importer maps them): $(echo "$pending" | tr '\n' ' ')"
fi

counts() { # one line per application table of the new database: name and rows
  psql_in -d cozy -At <<'SQL'
SELECT format('SELECT %L || '' '' || count(*) FROM %I', tablename, tablename)
FROM pg_tables WHERE schemaname = 'public' AND tablename NOT IN ('goose_db_version', 'job_lease')
ORDER BY tablename \gexec
SQL
}

echo "== Import"
status=0
timed "import" app_import > "$work/import.txt" 2>&1 || status=$?
if [ -n "$pending" ]; then
  # A pending table must make the real import refuse, and leave the target empty.
  if [ "$status" = 0 ]; then fail "the import committed although tables were pending"; fi
  grep -q "no mapping yet" "$work/import.txt" || fail "the import didn't refuse for pending tables: $(tail -n 1 "$work/import.txt")"
  [ -z "$(counts | awk '$2 != 0')" ] || fail "a refused import left rows in the target"
  echo "Refused, as it must while tables are pending: $(tail -n 1 "$work/import.txt")"
else
  cat "$work/import.txt"
  [ "$status" = 0 ] || fail "the import exited $status"
  grep -q "Imported and committed." "$work/import.txt" || fail "the import didn't commit"
  [ -z "$(marked MISMATCH "$work/import.txt")" ] || fail "the import's report has mismatches"

  echo "== Points per child, old and new (independent of the importer's checks)"
  # New balance = old balance + the points of spends on rewards still waiting for an answer,
  # which aren't carried (their spend comes back).
  psql_in -d old -At -c "SELECT l.child_id || ' ' || sum(l.delta) FROM opencozy.point_ledger l
    LEFT JOIN opencozy.reward_redemption r ON r.household_id = l.household_id AND r.id = l.redemption_id
    WHERE NOT coalesce(l.reward_entry_type = 'spend' AND r.state = 'pending', false)
    GROUP BY l.child_id ORDER BY l.child_id" > "$work/balance-old.txt"
  psql_in -d cozy -At -c "SELECT child_id || ' ' || sum(delta) FROM point_ledger GROUP BY child_id ORDER BY child_id" > "$work/balance-new.txt"
  paste -d ' ' "$work/balance-old.txt" "$work/balance-new.txt" | awk '{ printf "  %s  old %s  new %s\n", $1, $2, $4 }'
  diff -q "$work/balance-old.txt" "$work/balance-new.txt" > /dev/null || fail "points per child differ: $(diff "$work/balance-old.txt" "$work/balance-new.txt" | tr '\n' ' ')"

  # Unused seconds of every banked-time credit per child, old against new: iPad (personal_credit
  # -> screen_time_credit) and TV (tv_credit -> tv_credit).
  banked() { # banked <label> <old table> <new table>
    echo "== Banked $1 seconds per child, old and new"
    psql_in -d old -At -c "SELECT child_id || ' ' || sum(seconds - used_seconds) FROM opencozy.$2
      GROUP BY child_id HAVING sum(seconds - used_seconds) > 0 ORDER BY child_id" > "$work/banked-old.txt"
    psql_in -d cozy -At -c "SELECT child_id || ' ' || sum(seconds - used_seconds) FROM $3
      GROUP BY child_id HAVING sum(seconds - used_seconds) > 0 ORDER BY child_id" > "$work/banked-new.txt"
    paste -d ' ' "$work/banked-old.txt" "$work/banked-new.txt" | awk '{ printf "  %s  old %s  new %s\n", $1, $2, $4 }'
    diff -q "$work/banked-old.txt" "$work/banked-new.txt" > /dev/null || fail "banked $1 seconds per child differ: $(diff "$work/banked-old.txt" "$work/banked-new.txt" | tr '\n' ' ')"
  }
  banked iPad personal_credit screen_time_credit
  banked TV tv_credit tv_credit
fi

echo "== Second import (must refuse a non-empty target, or change nothing)"
counts > "$work/counts-before.txt"
status=0
timed "second import" app_import > "$work/again.txt" 2>&1 || status=$?
counts > "$work/counts-after.txt"
diff -q "$work/counts-before.txt" "$work/counts-after.txt" > /dev/null || fail "the second import changed the target"
if [ -n "$pending" ]; then
  # The target is still empty, so the second run refuses for the same reason as the first.
  grep -q "no mapping yet" "$work/again.txt" || fail "the second import didn't refuse: $(tail -n 1 "$work/again.txt")"
else
  [ "$status" != 0 ] || fail "the second import succeeded on a non-empty target"
  grep -q "the target is not empty" "$work/again.txt" || fail "the second import didn't refuse the non-empty target: $(tail -n 1 "$work/again.txt")"
fi
echo "Refused: $(tail -n 1 "$work/again.txt")"

echo "== Timings"
printf '  %s\n' "${timings[@]}"

if [ "${#failures[@]}" != 0 ]; then
  echo "== Rehearsal FAILED"
  printf '  - %s\n' "${failures[@]}"
  exit 1
fi
if [ "${ALLOW_PENDING:-}" = 1 ]; then
  echo "== Rehearsal passed with ALLOW_PENDING=1 (pending.sql left out): not yet the full rehearsal"
else
  echo "== Rehearsal passed"
fi
