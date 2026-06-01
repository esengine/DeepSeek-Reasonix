#!/usr/bin/env bash
set -euo pipefail

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

set +e
REASONIX_RELEASE_CACHE_GUARD=1 go test ./internal/agent -run '^TestReleaseCacheHitGuard$' -v -count=1 2>&1 | tee "$tmp"
status=${PIPESTATUS[0]}
set -e

if [ "$status" -ne 0 ]; then
  exit "$status"
fi

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  {
    echo "### Cache guard"
    echo
    echo '```'
    grep 'CACHE_GUARD_RESULT:' "$tmp" || true
    grep 'CACHE_GUARD_WARNING:' "$tmp" || true
    echo '```'
  } >> "$GITHUB_STEP_SUMMARY"
fi

while IFS= read -r line; do
  msg=${line#*CACHE_GUARD_WARNING: }
  echo "::warning title=Reasonix cache guard::$msg"
done < <(grep 'CACHE_GUARD_WARNING:' "$tmp" || true)
