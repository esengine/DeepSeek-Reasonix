#!/usr/bin/env bash
# Run the real 1.x readers/writers against each other's disposable framed stores.
set -euo pipefail
root=$(cd "$(dirname "$0")/.." && pwd)
scratch=$(mktemp -d "${TMPDIR:-/tmp}/reasonix-provider-compat.XXXXXX")
trap 'rm -rf "$scratch"' EXIT
fixture=internal/session/provider_diagnostic_compat_test.go
run_fixture() {
  (cd "$1" && REASONIX_COMPAT_STORE="$2" REASONIX_COMPAT_MODE="$3" REASONIX_COMPAT_KIND="$4" \
    go test -p 2 ./internal/session -run '^TestProviderDiagnosticCrossVersionFixture$' -count=1)
}
for version in v1.39.8 v1.39.9; do
  snapshot="$scratch/$version"
  mkdir -p "$snapshot"
  git -C "$root" archive "$version" | tar -x -C "$snapshot"
  cp "$root/$fixture" "$snapshot/$fixture"
  for kind in request_send_timeout image_input; do
    run_fixture "$root" "$scratch/new-to-$version-$kind" write "$kind"
    run_fixture "$snapshot" "$scratch/new-to-$version-$kind" read "$kind"
  done
  run_fixture "$snapshot" "$scratch/$version-to-new" write temporary
  run_fixture "$root" "$scratch/$version-to-new" read temporary
  printf 'PASS %s: bidirectional replay, continued write and reopen\n' "$version"
done
