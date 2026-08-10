#!/usr/bin/env bash
# The Wails CLI and the Wails library must be the same version.
#
# They drift silently in both directions. Dependabot bumps the library in
# desktop/go.mod but cannot see the CLI, whose version is a string inside three
# workflow files; and `wails dev` rewrites desktop/go.mod down to whatever CLI
# the developer has installed, so a local run can quietly revert a dependency
# upgrade. One pin, read by everyone, and this check to prove it held.
set -euo pipefail

root="${1:-$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)}"
pin="$(tr -d '[:space:]' < "$root/.wails-version")"
mod="$(awk '/github.com\/wailsapp\/wails\/v2 v/ {print $2; exit}' "$root/desktop/go.mod")"
module="github.com/wailsapp/wails/v2/cmd/wails"

if [ -z "$pin" ]; then
  echo "check-wails-pin: .wails-version is empty" >&2
  exit 1
fi
if [[ ! "$pin" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "check-wails-pin: .wails-version must contain one stable semantic version (vMAJOR.MINOR.PATCH), got: $pin" >&2
  exit 1
fi
if [ "$pin" != "$mod" ]; then
  cat >&2 <<MSG
check-wails-pin: version drift
  .wails-version   $pin
  desktop/go.mod   $mod

Running \`wails dev\` with an older CLI rewrites desktop/go.mod to match it. If the
library upgrade is intended, bump .wails-version too so CI installs the same CLI;
otherwise restore go.mod (git checkout -- desktop/go.mod desktop/go.sum).
MSG
  exit 1
fi

for workflow in "$root"/.github/workflows/ci.yml "$root"/.github/workflows/release-desktop.yml; do
  if grep -q "cmd/wails@v" "$workflow"; then
    echo "check-wails-pin: $(basename "$workflow") hard-codes a CLI version; read .wails-version instead" >&2
    exit 1
  fi
  # Several desktop steps run with working-directory: desktop, where a bare
  # `cat .wails-version` resolves to nothing and installs wails@"".
  if grep -q 'cat \.wails-version' "$workflow"; then
    echo "check-wails-pin: $(basename "$workflow") reads .wails-version relative to the job's working directory; use \$GITHUB_WORKSPACE" >&2
    exit 1
  fi
done

# The workflows are not the only way developers build a release. Reject every
# tracked install source except the three supported ways to read the shared pin.
install_lines="$(git -C "$root" grep -nF "$module@" -- . || true)"
unexpected_sources="$(awk -v module="$module" '
  index($0, ".github/workflows/ci.yml:") == 1 && index($0, module "@$(cat \"$GITHUB_WORKSPACE/.wails-version\")") { next }
  index($0, ".github/workflows/release-desktop.yml:") == 1 && index($0, module "@$(cat \"$GITHUB_WORKSPACE/.wails-version\")") { next }
  index($0, "Makefile:") == 1 && index($0, module "@$(WAILS_VERSION)") { next }
  index($0, "prod_test:") == 1 && index($0, module "@$wails_pin") { next }
  { print }
' <<<"$install_lines")"
if [[ -n "$unexpected_sources" ]]; then
  cat >&2 <<MSG
check-wails-pin: Wails CLI installs must read the shared .wails-version pin; found unsupported sources:
$unexpected_sources
MSG
  exit 1
fi

# Keep setup docs from bypassing the shared pin too. The escaped dots keep this
# regex from matching its own source.
hard_coded="$(git -C "$root" grep -nE 'github\.com/wailsapp/wails/v2/cmd/wails@(v[0-9]|latest)' -- . || true)"
if [[ -n "$hard_coded" ]]; then
  cat >&2 <<MSG
check-wails-pin: Wails CLI installs must read .wails-version; found:
$hard_coded
MSG
  exit 1
fi

workflow_install="go install \"$module@\$(cat \"\$GITHUB_WORKSPACE/.wails-version\")\""
for workflow in "$root"/.github/workflows/ci.yml "$root"/.github/workflows/release-desktop.yml; do
  grep -Fq "$workflow_install" "$workflow" || {
    echo "check-wails-pin: $(basename "$workflow") must install Wails from \$GITHUB_WORKSPACE/.wails-version" >&2
    exit 1
  }
done
grep -Fq 'WAILS_VERSION := $(shell tr -d '\''[:space:]'\'' < .wails-version)' "$root/Makefile" || {
  echo "check-wails-pin: Makefile must read WAILS_VERSION from .wails-version" >&2
  exit 1
}
grep -Fq "go install \"$module@\$(WAILS_VERSION)\"" "$root/Makefile" || {
  echo "check-wails-pin: make wails-install must install WAILS_VERSION" >&2
  exit 1
}
grep -Fq 'make wails-install' "$root/desktop/README.md" || {
  echo "check-wails-pin: desktop/README.md must direct developers to make wails-install" >&2
  exit 1
}
grep -Fq 'wails version' "$root/prod_test" || {
  echo "check-wails-pin: prod_test must verify the installed Wails CLI version" >&2
  exit 1
}
grep -Fq "go install \"$module@\$wails_pin\"" "$root/prod_test" || {
  echo "check-wails-pin: prod_test must install the shared Wails pin" >&2
  exit 1
}

echo "check-wails-pin: CLI and library both pinned at $pin"
