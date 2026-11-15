#!/usr/bin/env bash
# Checks a built app binary's own build info (go version -m) is a release build: built with
# embedweb and without the e2e tag, whose calendar client may reach 127.0.0.1. CI runs it on the
# binary taken out of the release image before pushing, and on the e2e binary expecting failure.
set -euo pipefail
bin=${1:?usage: check-release-binary.sh path/to/app}
info=$(go version -m "$bin")
tags=$(printf '%s\n' "$info" | awk -F'\t' '$2 == "build" && $3 ~ /^-tags=/ { sub(/^-tags=/, "", $3); print $3 }')
if [[ -z "$tags" ]]; then
  echo "$bin: no -tags in its build info; a release build has embedweb" >&2
  exit 1
fi
IFS=, read -r -a list <<<"$tags"
release=false
for t in "${list[@]}"; do
  case "$t" in
    e2e) echo "$bin: built with the e2e tag (-tags=$tags); never ship it" >&2; exit 1 ;;
    embedweb) release=true ;;
  esac
done
if [[ $release != true ]]; then
  echo "$bin: built without embedweb (-tags=$tags)" >&2
  exit 1
fi
echo "$bin: release build (-tags=$tags)"
