#!/usr/bin/env bash
set -euo pipefail

root="${1:-$(git rev-parse --show-toplevel)}"
required=(
	.github/actions/setup-certum/action.yml
	.github/workflows/release-candidate.yml
	.github/workflows/release-candidate-verify.yml
	.github/workflows/release-promote.yml
	.github/workflows/release-site-recovery.yml
	.github/workflows/release-desktop.yml
	.signpath/contracts/release-signing.yml
	scripts/build-release-cli-candidate.mjs
	scripts/desktop-release-artifacts.mjs
	scripts/finalize-windows-signed-candidate.sh
	scripts/install-nsis.ps1
	scripts/package-windows-desktop.sh
	scripts/release-candidate.mjs
	scripts/release-candidate-tags.sh
	scripts/test-release-control-contracts.sh
	scripts/sync-release-site.sh
	scripts/observe-release-site.sh
	scripts/release-publication-ledger.mjs
	scripts/check-release-public-access.sh
	scripts/fetch-stable-release-manifest.sh
	scripts/release-manifest-fetch/main.go
	scripts/resolve-release-candidate.mjs
	scripts/sign-certum.ps1
	scripts/test-windows-installer-startup.ps1
	scripts/test-windows-startup-recovery.ps1
	scripts/test-windows-upgrade-startup.ps1
	scripts/verify-windows-authenticode.ps1
	scripts/verify-release-authorization.sh
	scripts/verify-release-artifact-archive.mjs
	scripts/windows-acceptance-environment.ps1
	scripts/windows-upgrade-ui-evidence.ps1
)

for file in "${required[@]}"; do
	if [ ! -s "$root/$file" ]; then
		echo "missing release control file: $file" >&2
		exit 1
	fi
done

for file in \
	scripts/finalize-windows-signed-candidate.sh \
	scripts/package-windows-desktop.sh; do
	if [ ! -x "$root/$file" ]; then
		echo "release control helper is not executable: $file" >&2
		exit 1
	fi
done

node --check "$root/scripts/release-candidate.mjs"
node --check "$root/scripts/verify-release-artifact-archive.mjs"
node --check "$root/scripts/resolve-release-candidate.mjs"
node --check "$root/scripts/build-release-cli-candidate.mjs"
bash -n "$root/scripts/finalize-windows-signed-candidate.sh"
bash -n "$root/scripts/release-candidate-tags.sh"
bash -n "$root/scripts/package-windows-desktop.sh"

bash "$root/scripts/test-release-control-contracts.sh" "$root"
echo "release control preflight: PASS"
