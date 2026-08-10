#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
scratch="$(mktemp -d)"
trap 'rm -rf "$scratch"' EXIT
wails_module="github.com/wailsapp/wails/v2/cmd/wails"
fixture_pin="v9.8.7"
repo_pin="$(tr -d '[:space:]' < "$root/.wails-version")"
fake_mismatch="v0.0.0"
if [[ "$fake_mismatch" == "$repo_pin" ]]; then
  fake_mismatch="v0.0.1"
fi

make_fixture() {
  local name="$1"
  local fixture="$scratch/$name"
  mkdir -p "$fixture/.github/workflows" "$fixture/desktop" "$fixture/scripts"
  cp "$root/scripts/check-wails-pin.sh" "$fixture/scripts/check-wails-pin.sh"
  printf '%s\n' "$fixture_pin" > "$fixture/.wails-version"
  printf 'github.com/wailsapp/wails/v2 %s\n' "$fixture_pin" > "$fixture/desktop/go.mod"
  printf 'run: go install "%s@$(cat "$GITHUB_WORKSPACE/.wails-version")"\n' "$wails_module" > "$fixture/.github/workflows/ci.yml"
  cp "$fixture/.github/workflows/ci.yml" "$fixture/.github/workflows/release-desktop.yml"
  printf 'go install "%s@$wails_pin"\nwails version\n' "$wails_module" > "$fixture/prod_test"
  printf 'Run `make wails-install`.\n' > "$fixture/desktop/README.md"
  printf 'WAILS_VERSION := $(shell tr -d '\''[:space:]'\'' < .wails-version)\nwails-install:\n\tgo install "%s@$(WAILS_VERSION)"\n' "$wails_module" > "$fixture/Makefile"
  git -C "$fixture" init -q
  git -C "$fixture" add .
  printf '%s' "$fixture"
}

expect_failure() {
  local fixture="$1"
  local expected="$2"
  local output
  if output="$(bash "$fixture/scripts/check-wails-pin.sh" "$fixture" 2>&1)"; then
    echo "check-wails-pin test: expected failure containing: $expected" >&2
    exit 1
  fi
  grep -Fq "$expected" <<<"$output" || {
    echo "check-wails-pin test: missing failure text: $expected" >&2
    echo "$output" >&2
    exit 1
  }
}

fixture="$(make_fixture valid)"
bash "$fixture/scripts/check-wails-pin.sh" "$fixture" >/dev/null

fixture="$(make_fixture prod-test-hard-code)"
printf 'go install %s@v2.12.0\n' "$wails_module" >> "$fixture/prod_test"
git -C "$fixture" add prod_test
expect_failure "$fixture" 'prod_test:'

fixture="$(make_fixture docs-latest)"
printf 'go install %s@latest\n' "$wails_module" >> "$fixture/desktop/README.md"
git -C "$fixture" add desktop/README.md
expect_failure "$fixture" 'desktop/README.md:'

fixture="$(make_fixture module-drift)"
printf 'github.com/wailsapp/wails/v2 v9.8.8\n' > "$fixture/desktop/go.mod"
expect_failure "$fixture" 'version drift'

fixture="$(make_fixture workflow-cwd)"
printf 'run: go install "%s@$(cat .wails-version)"\n' "$wails_module" > "$fixture/.github/workflows/ci.yml"
expect_failure "$fixture" "reads .wails-version relative to the job's working directory"

fixture="$(make_fixture workflow-other-source)"
printf 'run: go install "%s@$WAILS_VERSION"\n' "$wails_module" > "$fixture/.github/workflows/ci.yml"
expect_failure "$fixture" 'unsupported sources'

fixture="$(make_fixture makefile-hard-code)"
printf 'WAILS_VERSION := %s\nwails-install:\n\tgo install "%s@$(WAILS_VERSION)"\n' "$fixture_pin" "$wails_module" > "$fixture/Makefile"
expect_failure "$fixture" 'Makefile must read WAILS_VERSION from .wails-version'

fixture="$(make_fixture invalid-pin)"
printf '%s;unexpected\n' "$fixture_pin" > "$fixture/.wails-version"
printf 'github.com/wailsapp/wails/v2 %s;unexpected\n' "$fixture_pin" > "$fixture/desktop/go.mod"
expect_failure "$fixture" '.wails-version must contain one stable semantic version'

fake_bin="$scratch/fake-bin"
mkdir -p "$fake_bin"
printf '#!/usr/bin/env bash\nprintf "%s\\n"\n' "$fake_mismatch" > "$fake_bin/wails"
printf '#!/usr/bin/env bash\nexit 0\n' > "$fake_bin/pnpm"
chmod +x "$fake_bin/wails" "$fake_bin/pnpm"
if output="$(GOBIN="$fake_bin" PATH="$fake_bin:$PATH" PROD_TEST_INSTALL_TOOLS=0 PROD_TEST_FAST=1 DESKTOP_BUILD_SKIP_DMG=1 PROD_TEST_OPEN_DIST=0 "$root/prod_test" darwin/arm64 2>&1)"; then
  echo "check-wails-pin test: prod_test accepted a mismatched installed Wails CLI" >&2
  exit 1
fi
grep -Fq "Wails CLI $fake_mismatch does not match .wails-version $repo_pin" <<<"$output" || {
  echo "check-wails-pin test: prod_test did not report the installed-version mismatch" >&2
  echo "$output" >&2
  exit 1
}

printf '#!/usr/bin/env bash\nexit 1\n' > "$fake_bin/wails"
if output="$(GOBIN="$fake_bin" PATH="$fake_bin:$PATH" PROD_TEST_INSTALL_TOOLS=0 PROD_TEST_FAST=1 DESKTOP_BUILD_SKIP_DMG=1 PROD_TEST_OPEN_DIST=0 "$root/prod_test" darwin/arm64 2>&1)"; then
  echo "check-wails-pin test: prod_test accepted a broken installed Wails CLI" >&2
  exit 1
fi
grep -Fq "Wails CLI not found does not match .wails-version $repo_pin" <<<"$output" || {
  echo "check-wails-pin test: prod_test did not report the broken installed CLI" >&2
  echo "$output" >&2
  exit 1
}

echo "check-wails-pin tests: ok"
