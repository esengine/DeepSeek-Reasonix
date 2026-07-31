#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
test_root="$(mktemp -d "${TMPDIR:-/tmp}/reasonix-release-workflow-test.XXXXXX")"
cleanup() {
	case "$test_root" in
	*/reasonix-release-workflow-test.*) rm -rf -- "$test_root" ;;
	*) echo "refusing to clean unexpected test directory: $test_root" >&2 ;;
	esac
}
trap cleanup EXIT

# Stable tags have one entrypoint and one protected environment. Reusable
# publishers must verify that only that entrypoint can claim prior approval.
[ "$(grep -Ec '^    environment: release$' "$repo_root/.github/workflows/release-stable.yml")" = "1" ]
for relay in release-stable-trigger.yml release-cli-trigger.yml release-desktop-trigger.yml; do
	grep -Eq 'actions: write' "$repo_root/.github/workflows/$relay"
	grep -Eq "CONTROL_PLANE_REF.*default_branch" "$repo_root/.github/workflows/$relay"
	grep -Eq "CONTROL_PLANE_REF !== 'main-v2'" "$repo_root/.github/workflows/$relay"
	grep -Eq 'createWorkflowDispatch' "$repo_root/.github/workflows/$relay"
	grep -Eq 'ref: process\.env\.CONTROL_PLANE_REF' "$repo_root/.github/workflows/$relay"
done
grep -Eq "workflow_id: 'release-stable\.yml'" \
	"$repo_root/.github/workflows/release-stable-trigger.yml"
grep -Eq "allow_recovery: 'false'" \
	"$repo_root/.github/workflows/release-stable-trigger.yml"
grep -Eq 'ALLOW_STABLE_RECOVERY:.*inputs\.allow_recovery' \
	"$repo_root/.github/workflows/release-stable.yml"
grep -Eq "workflow_id: 'release-desktop\.yml'" \
	"$repo_root/.github/workflows/release-desktop-trigger.yml"
grep -Fq 'Desktop Preview must be dispatched without a Git tag' \
	"$repo_root/.github/workflows/release-desktop-trigger.yml"
grep -Fq "workflow_id: preview ? 'release-preview.yml' : 'release.yml'" \
	"$repo_root/.github/workflows/release-cli-trigger.yml"
grep -Eq "preview\\\\\." "$repo_root/.github/workflows/release-cli-trigger.yml"
grep -Eq '^name: Release preview$' "$repo_root/.github/workflows/release-preview.yml"
grep -Eq '^  group: preview-release$' "$repo_root/.github/workflows/release-preview.yml"
if grep -Fq 'group: preview-release-${{ inputs.tag }}' "$repo_root/.github/workflows/release-preview.yml"; then
	echo "Preview releases must serialize across versions" >&2
	exit 1
fi
grep -Eq '^    environment: canary$' "$repo_root/.github/workflows/release-preview.yml"
grep -Eq '^  signpath-preflight:$' "$repo_root/.github/workflows/release-preview.yml"
grep -Eq 'signing_preflight: true' "$repo_root/.github/workflows/release-preview.yml"
grep -Eq 'signing_preflight_verified: true' "$repo_root/.github/workflows/release-preview.yml"
[ "$(grep -Ec 'needs: \[authorize, signpath-preflight\]' "$repo_root/.github/workflows/release-preview.yml")" = "3" ]
grep -Eq 'release-event\.mjs generate' "$repo_root/.github/workflows/release-preview.yml"
grep -Eq 'gh workflow run pages\.yml' "$repo_root/.github/workflows/release-preview.yml"
grep -Eq 'release-cli-trigger\.yml' "$repo_root/.github/workflows/ci.yml"
if grep -Eq '^  push:$' "$repo_root/.github/workflows/release-stable.yml" ||
	grep -Eq '^  push:$' "$repo_root/.github/workflows/release.yml" ||
	grep -Eq '^  push:$' "$repo_root/.github/workflows/release-desktop.yml"; then
	echo "production workflow must be dispatched on protected main-v2, not run on a tag origin" >&2
	exit 1
fi
for workflow in release.yml release-npm.yml release-desktop.yml; do
	grep -Eq 'github\.workflow_ref' "$repo_root/.github/workflows/$workflow"
	grep -Eq 'github\.ref_protected' "$repo_root/.github/workflows/$workflow"
	grep -Eq 'inputs\.approved_sha' "$repo_root/.github/workflows/$workflow"
	grep -Eq 'verify-release-tag\.sh' "$repo_root/.github/workflows/$workflow"
	grep -Eq 'release-\{1\}\.yml' "$repo_root/.github/workflows/$workflow"
	grep -Eq "needs\.cache-guard\.result == 'success'" "$repo_root/.github/workflows/$workflow"
done
grep -Eq 'options: \[stable\]' "$repo_root/.github/workflows/release.yml"
grep -A6 -E '^      channel:' "$repo_root/.github/workflows/release.yml" |
	grep -Eq 'default: stable'
grep -Eq '^    environment: release$' "$repo_root/.github/workflows/release.yml"
grep -Eq 'GORELEASER_CURRENT_TAG:.*needs\.resolve\.outputs\.tag' \
	"$repo_root/.github/workflows/release.yml"
grep -Eq 'bash scripts/resolve-cli-release\.sh' "$repo_root/.github/workflows/release.yml"
grep -Eq 'git merge-base --is-ancestor.*origin/main-v2' "$repo_root/.github/workflows/release.yml"
grep -Eq 'CLI Preview must tag current main-v2' "$repo_root/.github/workflows/release.yml"
grep -Eq "channel == 'stable'.*HOMEBREW_TAP_TOKEN" "$repo_root/.github/workflows/release.yml"
grep -Eq "needs\.build\.result == 'success'" "$repo_root/.github/workflows/release-desktop.yml"
grep -Eq "needs\.publish\.result == 'success'" "$repo_root/.github/workflows/release-desktop.yml"
grep -Eq 'options: \[stable, preview\]' "$repo_root/.github/workflows/release-desktop.yml"
grep -Eq '^  resolve:$' "$repo_root/.github/workflows/release-desktop.yml"
grep -Eq 'sha:.*steps\.candidate\.outputs\.sha' "$repo_root/.github/workflows/release-desktop.yml"
grep -Fq 'bash scripts/resolve-desktop-candidate.sh' "$repo_root/.github/workflows/release-desktop.yml"
[ "$(grep -Fc 'name: Revalidate immutable Desktop candidate' "$repo_root/.github/workflows/release-desktop.yml")" = "2" ]
[ "$(grep -Fc 'ref: ${{ needs.resolve.outputs.sha }}' "$repo_root/.github/workflows/release-desktop.yml")" -ge 4 ]
[ "$(grep -Fc 'path: release-control' "$repo_root/.github/workflows/release-desktop.yml")" = "2" ]
[ "$(grep -Fc 'ref: ${{ github.workflow_sha }}' "$repo_root/.github/workflows/release-desktop.yml")" -ge 2 ]
[ "$(grep -Fc 'bash release-control/scripts/resolve-desktop-candidate.sh' "$repo_root/.github/workflows/release-desktop.yml")" = "2" ]
if sed -n '/name: publish release/,/name: mirror to R2/p' \
	"$repo_root/.github/workflows/release-desktop.yml" |
	grep -Eq 'bash scripts/(resolve-desktop-candidate|validate-desktop-release-manifest|publish-desktop-github-release)\.sh'; then
	echo "Desktop publish job uses candidate-controlled release scripts" >&2
	exit 1
fi
if sed -n '/name: mirror to R2/,$p' \
	"$repo_root/.github/workflows/release-desktop.yml" |
	grep -Eq 'bash scripts/(resolve-desktop-candidate|validate-desktop-release-manifest|verify-desktop-release-directory|decide-desktop-pointer-update)\.sh'; then
	echo "Desktop mirror job uses candidate-controlled release scripts" >&2
	exit 1
fi
if grep -Fq 'ref: ${{ inputs.approved_sha || inputs.tag || github.ref }}' \
	"$repo_root/.github/workflows/release-desktop.yml"; then
	echo "Desktop jobs must checkout the resolved immutable candidate SHA" >&2
	exit 1
fi
grep -Fq "group: release-desktop-\${{ (inputs.channel == 'preview' || inputs.channel == 'canary') && 'preview' || 'stable' }}" \
	"$repo_root/.github/workflows/release-desktop.yml"
grep -Eq 'IN_PRODUCTION_SIGNING_SMOKE:.*inputs\.production_signing_smoke' \
	"$repo_root/.github/workflows/release-desktop.yml"
grep -Eq 'IN_SIGNING_PREFLIGHT:.*inputs\.signing_preflight' \
	"$repo_root/.github/workflows/release-desktop.yml"
grep -Fq "group: release-npm-\${{ inputs.channel || 'next' }}" \
	"$repo_root/.github/workflows/release-npm.yml"
sed -n '/^  workflow_dispatch:/,/^  workflow_call:/p' "$repo_root/.github/workflows/release-npm.yml" |
	grep -Eq 'default: stable'
if sed -n '/^  workflow_dispatch:/,/^  workflow_call:/p' "$repo_root/.github/workflows/release-npm.yml" |
	grep -Eq '^          - canary$'; then
	echo "Standalone npm dispatch must not expose public Canary publication" >&2
	exit 1
fi
grep -Fq 'if: ${{ !inputs.orchestrated }}' "$repo_root/.github/workflows/release-npm.yml"
grep -Fq 'Publish or recover immutable npm packages' "$repo_root/.github/workflows/release-npm.yml"
grep -Fq 'publishPackages' "$repo_root/npm/build.mjs"
grep -Eq 'signing-policy-slug: release-signing' "$repo_root/.github/workflows/release-desktop.yml"
if grep -Eq 'signing-policy-slug:.*test-signing' "$repo_root/.github/workflows/release-desktop.yml"; then
	echo "public desktop workflow must not use the SignPath test certificate" >&2
	exit 1
fi
grep -Eq 'SIGNPATH_RELEASE_SIGNING_ATTESTATION does not match' "$repo_root/.github/workflows/release-desktop.yml"
grep -Eq '^      signing_preflight:$' "$repo_root/.github/workflows/release-desktop.yml"
grep -Eq '^      signing_preflight_verified:$' "$repo_root/.github/workflows/release-desktop.yml"
grep -Eq '^  signing-contract:$' "$repo_root/.github/workflows/release-desktop.yml"
grep -Eq '^  attest-signing-contract:$' "$repo_root/.github/workflows/release-desktop.yml"
grep -Eq '^      production_signing_smoke:$' "$repo_root/.github/workflows/release-desktop.yml"
grep -Eq "needs\.build\.result == 'success'.*!inputs\.production_signing_smoke.*!inputs\.signing_preflight" \
	"$repo_root/.github/workflows/release-desktop.yml"
[ "$(grep -Ec 'wait-for-completion: false' "$repo_root/.github/workflows/release-desktop.yml")" = "2" ]
[ "$(grep -Ec 'complete-signpath-request\.ps1' "$repo_root/.github/workflows/release-desktop.yml")" = "2" ]
[ "$(grep -Ec -- '-WaitForExternalApproval:\$waitForExternalApproval' "$repo_root/.github/workflows/release-desktop.yml")" = "2" ]
[ "$(grep -Ec 'signpath-api-url' "$repo_root/.github/workflows/release-desktop.yml")" = "0" ]
grep -Eq 'steps\.submit-windows-payload\.outputs\.signing-request-id' \
	"$repo_root/.github/workflows/release-desktop.yml"
grep -Eq 'steps\.submit-windows-installer\.outputs\.signing-request-id' \
	"$repo_root/.github/workflows/release-desktop.yml"
grep -Eq 'artifact-configuration-slug: windows-payload' "$repo_root/.github/workflows/release-desktop.yml"
grep -Eq 'artifact-configuration-slug: windows-installer-v2' "$repo_root/.github/workflows/release-desktop.yml"
grep -Fq -- '-RequireTrusted:$true' "$repo_root/.github/workflows/release-desktop.yml"
grep -Eq '^  signpath-preflight:$' "$repo_root/.github/workflows/release-stable.yml"
grep -Eq 'signing_preflight: true' "$repo_root/.github/workflows/release-stable.yml"
grep -Eq 'signing_preflight_verified: true' "$repo_root/.github/workflows/release-stable.yml"
grep -Eq 'needs: \[authorize, signpath-preflight\]' "$repo_root/.github/workflows/release-stable.yml"
grep -Eq 'name: Require R2 for Preview' "$repo_root/.github/workflows/release-desktop.yml"
grep -Eq "channel == 'preview'.*HAS_R2 != 'true'" "$repo_root/.github/workflows/release-desktop.yml"
grep -Eq 'name: Validate generated manifest before publication' "$repo_root/.github/workflows/release-desktop.yml"
grep -Eq 'name: Validate R2 manifest before upload' "$repo_root/.github/workflows/release-desktop.yml"
grep -Fq 'scripts/publish-desktop-github-release.sh' "$repo_root/.github/workflows/release-desktop.yml"
if grep -Eq 'gh release create .*dist/' "$repo_root/.github/workflows/release-desktop.yml"; then
	echo "Desktop release publication must recover an existing GitHub release" >&2
	exit 1
fi
grep -Fq 'scripts/decide-desktop-pointer-update.sh' "$repo_root/.github/workflows/release-desktop.yml"
grep -Fq 'scripts/verify-desktop-release-directory.sh' "$repo_root/.github/workflows/release-desktop.yml"
grep -Fq 'scripts/compare-desktop-release-manifests.sh' "$repo_root/.github/workflows/release-desktop.yml"
grep -Fq 'download_optional "preview/latest.json"' "$repo_root/.github/workflows/release-desktop.yml"
grep -Fq 'download_optional "canary/latest.json"' "$repo_root/.github/workflows/release-desktop.yml"
grep -Fq 'publish_pointer canary' "$repo_root/.github/workflows/release-desktop.yml"
grep -Fq 'publish_pointer preview' "$repo_root/.github/workflows/release-desktop.yml"
grep -Fq 'cmp -s /tmp/reasonix-desktop-canary-latest.json /tmp/reasonix-desktop-preview-latest.json' \
	"$repo_root/.github/workflows/release-desktop.yml"
grep -Fq 'steps.mirror_r2.outputs.pointer_moved' "$repo_root/.github/workflows/release-desktop.yml"
if grep -Fq 'aws s3 cp assets/ "s3://${R2_BUCKET}/preview/"' "$repo_root/.github/workflows/release-desktop.yml" ||
	grep -Fq 'aws s3 cp assets/ "s3://${R2_BUCKET}/canary/"' "$repo_root/.github/workflows/release-desktop.yml"; then
	echo "Desktop public pointers must contain only latest.json, not mutable asset copies" >&2
	exit 1
fi

desktop_generated_validation_line="$(
	grep -n -m1 'name: Validate generated manifest before publication' \
		"$repo_root/.github/workflows/release-desktop.yml" | cut -d: -f1
)"
desktop_github_publish_line="$(
	grep -n -m1 'name: Publish GitHub release' \
		"$repo_root/.github/workflows/release-desktop.yml" | cut -d: -f1
)"
desktop_r2_validation_line="$(
	grep -n -m1 'name: Validate R2 manifest before upload' \
		"$repo_root/.github/workflows/release-desktop.yml" | cut -d: -f1
)"
desktop_r2_upload_line="$(
	grep -n -m1 'name: Mirror immutable assets and advance R2 pointer' \
		"$repo_root/.github/workflows/release-desktop.yml" | cut -d: -f1
)"
[ "$desktop_generated_validation_line" -lt "$desktop_github_publish_line" ]
[ "$desktop_r2_validation_line" -lt "$desktop_r2_upload_line" ]
grep -Eq '^  postflight:$' "$repo_root/.github/workflows/release-stable.yml"
grep -Eq 'verify-stable-release-artifacts\.sh' "$repo_root/.github/workflows/release-stable.yml"
grep -Eq 'name: Upload reviewed release notes' "$repo_root/.github/workflows/release-stable.yml"
grep -Eq 'name: orchestrator-reviewed-release-notes' "$repo_root/.github/workflows/release-stable.yml"
grep -Eq 'name: Upload reviewed release notes' "$repo_root/.github/workflows/release-preview.yml"
grep -Eq 'name: orchestrator-reviewed-release-notes' "$repo_root/.github/workflows/release-preview.yml"
for channel in cli npm desktop; do
	grep -Eq '^      publish_'"$channel"':' "$repo_root/.github/workflows/release-stable.yml"
	grep -Eq "inputs\.publish_$channel" "$repo_root/.github/workflows/release-stable.yml"
done

# The checked-in SignPath policy contract is the single source of truth for the
# provider allowlist and every top-level workflow that can reach signing.
go run ./cmd/signpath-contract validate
contract_fingerprint="$(go run ./cmd/signpath-contract fingerprint)"
grep -Eq '^v1:[0-9a-f]{64}$' <<<"$contract_fingerprint"
grep -Eq 'public postflight will still verify it' "$repo_root/.github/workflows/release-stable.yml"
for workflow in release.yml release-desktop.yml; do
	grep -Eq 'name: Download orchestrator-reviewed release notes' "$repo_root/.github/workflows/$workflow"
	grep -Eq 'name: orchestrator-reviewed-release-notes' "$repo_root/.github/workflows/$workflow"
	grep -Eq 'if: \$\{\{ !inputs\.orchestrated' "$repo_root/.github/workflows/$workflow"
done

# CLI clients and the website consume separate Stable and Preview pointers. The
# publisher must validate every public archive before moving a pointer, retain an
# immutable per-tag record, and leave both public pointers untouched for RCs.
cli_release_workflow="$repo_root/.github/workflows/release.yml"
grep -Eq 'name: Publish CLI release metadata to R2' "$cli_release_workflow"
grep -Fq 'cli/releases/${TAG}/latest.json' "$cli_release_workflow"
grep -Fq 'cli/${channel}/latest.json' "$cli_release_workflow"
grep -Fq "group: release-cli-\${{ inputs.channel || 'stable' }}" "$cli_release_workflow"
grep -Fq 'scripts/decide-cli-pointer-update.sh' "$cli_release_workflow"
grep -Fq 'scripts/validate-cli-release-manifest.sh' "$cli_release_workflow"
grep -Fq 'scripts/compare-cli-release-manifests.sh' "$cli_release_workflow"
grep -Fq 'immutable CLI release metadata for $TAG already exists with different content' "$cli_release_workflow"
grep -Fq 'cmp -s /tmp/cli-release.json /tmp/cli-release.pointer.json' "$cli_release_workflow"
grep -Eq 'internal CLI release .*Stable and Preview pointers remain unchanged' "$cli_release_workflow"
cli_public_validation_line="$(
	grep -n -m1 'if \[ -n "\$channel" \]' "$cli_release_workflow" | cut -d: -f1
)"
cli_immutable_upload_line="$(
	grep -n -m1 'cli/releases/${TAG}/latest.json' "$cli_release_workflow" | cut -d: -f1
)"
[ "$cli_public_validation_line" -lt "$cli_immutable_upload_line" ]
pointer_compare="$repo_root/scripts/compare-cli-release-tags.sh"
test -x "$pointer_compare"
[ "$(bash "$pointer_compare" stable v1.2.4 v1.2.3)" = "update" ]
[ "$(bash "$pointer_compare" stable v1.2.3 v1.2.3)" = "skip" ]
[ "$(bash "$pointer_compare" stable v1.2.2 v1.2.3)" = "skip" ]
[ "$(bash "$pointer_compare" stable v100000000000000000000.0.0 v99999999999999999999.999.999)" = "update" ]
[ "$(bash "$pointer_compare" preview v1.2.3-preview.11 v1.2.3-preview.9)" = "update" ]
[ "$(bash "$pointer_compare" preview v1.2.3-preview.9 v1.2.3-preview.9)" = "skip" ]
[ "$(bash "$pointer_compare" preview v1.2.3-preview.8 v1.2.3-preview.9)" = "skip" ]
[ "$(bash "$pointer_compare" stable v1.2.3 "")" = "update" ]
if bash "$pointer_compare" stable v1.2.3 v1.2.3-preview.1 >/dev/null 2>&1; then
	echo "stable comparator accepted a preview pointer" >&2
	exit 1
fi
if bash "$pointer_compare" preview v1.2.3-preview.1 v1.2.3 >/dev/null 2>&1; then
	echo "preview comparator accepted a stable pointer" >&2
	exit 1
fi
if bash "$pointer_compare" stable v01.2.3 v1.2.2 >/dev/null 2>&1; then
	echo "stable comparator accepted a non-canonical tag" >&2
	exit 1
fi
cli_pointer_decider="$repo_root/scripts/decide-cli-pointer-update.sh"
test -x "$cli_pointer_decider"
cli_pointer_candidate="$test_root/cli-pointer-candidate.json"
cli_pointer_older="$test_root/cli-pointer-older.json"
cli_pointer_equal="$test_root/cli-pointer-equal.json"
cli_pointer_equal_different="$test_root/cli-pointer-equal-different.json"
cli_pointer_newer="$test_root/cli-pointer-newer.json"
printf '{"tag_name":"v1.3.0","marker":"candidate"}\n' >"$cli_pointer_candidate"
printf '{"tag_name":"v1.2.9","marker":"older"}\n' >"$cli_pointer_older"
cp "$cli_pointer_candidate" "$cli_pointer_equal"
printf '{"tag_name":"v1.3.0","marker":"different"}\n' >"$cli_pointer_equal_different"
printf '{"tag_name":"v1.3.1","marker":"newer"}\n' >"$cli_pointer_newer"
[ "$(bash "$cli_pointer_decider" stable "$cli_pointer_candidate" -)" = "update" ]
[ "$(bash "$cli_pointer_decider" stable "$cli_pointer_candidate" "$cli_pointer_older")" = "update" ]
[ "$(bash "$cli_pointer_decider" stable "$cli_pointer_candidate" "$cli_pointer_equal")" = "skip" ]
[ "$(bash "$cli_pointer_decider" stable "$cli_pointer_candidate" "$cli_pointer_equal_different")" = "update" ]
[ "$(bash "$cli_pointer_decider" stable "$cli_pointer_candidate" "$cli_pointer_newer")" = "skip" ]
if bash "$cli_pointer_decider" stable "$cli_pointer_candidate" "$test_root/missing-cli-pointer.json" >/dev/null 2>&1; then
	echo "CLI pointer decider accepted an unreadable manifest path" >&2
	exit 1
fi
for asset in \
	reasonix-darwin-amd64.tar.gz \
	reasonix-darwin-arm64.tar.gz \
	reasonix-linux-amd64.tar.gz \
	reasonix-linux-arm64.tar.gz \
	reasonix-windows-amd64.zip \
	reasonix-windows-arm64.zip \
	SHA256SUMS; do
	grep -Fq "\"$asset\"" "$cli_release_workflow"
done
manifest_validator="$repo_root/scripts/validate-cli-release-manifest.sh"
manifest_comparator="$repo_root/scripts/compare-cli-release-manifests.sh"
test -x "$manifest_validator"
test -x "$manifest_comparator"
manifest_assets='[
	"reasonix-darwin-amd64.tar.gz",
	"reasonix-darwin-arm64.tar.gz",
	"reasonix-linux-amd64.tar.gz",
	"reasonix-linux-arm64.tar.gz",
	"reasonix-windows-amd64.zip",
	"reasonix-windows-arm64.zip",
	"SHA256SUMS"
]'
manifest_repo="esengine/DeepSeek-Reasonix"
manifest_tag="v1.2.3"
manifest_file="$test_root/cli-release-manifest.json"
jq -n \
	--arg repo "$manifest_repo" \
	--arg tag "$manifest_tag" \
	--argjson names "$manifest_assets" '
	{
		tag_name: $tag,
		prerelease: false,
		html_url: ("https://github.com/" + $repo + "/releases/tag/" + $tag),
		release_notes_url: ("https://reasonix.io/changelog/" + $tag + "/"),
		assets: [
			$names[] as $name |
			{
				name: $name,
				browser_download_url:
					("https://github.com/" + $repo + "/releases/download/" + $tag + "/" + $name),
				size: 1
			}
		]
	}
' >"$manifest_file"
bash "$manifest_validator" stable "$manifest_tag" "$manifest_repo" "$manifest_file"
bash "$manifest_validator" any "$manifest_tag" "$manifest_repo" "$manifest_file"

legacy_manifest="$test_root/cli-release-manifest-legacy.json"
jq 'del(.release_notes_url)' "$manifest_file" >"$legacy_manifest"
bash "$manifest_validator" legacy-stable "$manifest_tag" "$manifest_repo" "$legacy_manifest"
bash "$manifest_comparator" "$manifest_file" "$legacy_manifest"
if bash "$manifest_validator" stable "$manifest_tag" "$manifest_repo" "$legacy_manifest" >/dev/null 2>&1; then
	echo "strict CLI manifest validator accepted a legacy manifest" >&2
	exit 1
fi
wrong_legacy_notes="$test_root/cli-release-manifest-wrong-legacy-notes.json"
jq '.release_notes_url = "https://reasonix.io/changelog/v9.9.9/"' \
	"$manifest_file" >"$wrong_legacy_notes"
if bash "$manifest_validator" legacy-stable "$manifest_tag" "$manifest_repo" \
	"$wrong_legacy_notes" >/dev/null 2>&1; then
	echo "legacy CLI manifest validator accepted a mismatched release-notes URL" >&2
	exit 1
fi
if bash "$manifest_comparator" "$manifest_file" "$wrong_legacy_notes" >/dev/null 2>&1; then
	echo "CLI manifest comparator ignored a non-legacy difference" >&2
	exit 1
fi

rc_manifest_tag="v1.2.4-rc.1"
rc_notes_tag="v1.2.4"
rc_manifest="$test_root/cli-release-manifest-rc.json"
jq \
	--arg repo "$manifest_repo" \
	--arg tag "$rc_manifest_tag" \
	--arg notes_tag "$rc_notes_tag" '
	.tag_name = $tag |
	.prerelease = true |
	.html_url = ("https://github.com/" + $repo + "/releases/tag/" + $tag) |
	.release_notes_url = ("https://reasonix.io/changelog/" + $notes_tag + "/") |
	.assets |= map(
		.browser_download_url =
			("https://github.com/" + $repo + "/releases/download/" + $tag + "/" + .name)
	)
' "$manifest_file" >"$rc_manifest"
bash "$manifest_validator" any "$rc_manifest_tag" "$manifest_repo" "$rc_manifest" "$rc_notes_tag"

expect_invalid_cli_manifest() {
	local description="$1"
	local file="$2"
	if bash "$manifest_validator" stable "$manifest_tag" "$manifest_repo" "$file" >/dev/null 2>&1; then
		echo "CLI manifest validator accepted $description" >&2
		exit 1
	fi
}

jq '.assets[0].browser_download_url = "https://github.com/attacker/project/releases/download/v1.2.3/reasonix-darwin-amd64.tar.gz"' \
	"$manifest_file" >"$test_root/wrong-repository.json"
expect_invalid_cli_manifest "an asset from another repository" "$test_root/wrong-repository.json"
jq '.assets[0].browser_download_url = "https://github.com/esengine/DeepSeek-Reasonix/releases/download/v9.9.9/reasonix-darwin-amd64.tar.gz"' \
	"$manifest_file" >"$test_root/wrong-tag.json"
expect_invalid_cli_manifest "an asset from another tag" "$test_root/wrong-tag.json"
jq '.assets = .assets[:-1]' "$manifest_file" >"$test_root/missing-asset.json"
expect_invalid_cli_manifest "a missing required asset" "$test_root/missing-asset.json"
jq '.assets[0].size = 0' "$manifest_file" >"$test_root/empty-asset.json"
expect_invalid_cli_manifest "a zero-byte asset" "$test_root/empty-asset.json"
jq '.assets[0].size = 1073741825' "$manifest_file" >"$test_root/oversized-asset.json"
expect_invalid_cli_manifest "an asset above the release size limit" "$test_root/oversized-asset.json"
jq '.assets[0].size = 9007199254740992' "$manifest_file" >"$test_root/unsafe-integer-asset.json"
expect_invalid_cli_manifest "an asset size above the JavaScript safe-integer range" \
	"$test_root/unsafe-integer-asset.json"
if bash "$manifest_validator" preview "$manifest_tag" "$manifest_repo" "$manifest_file" >/dev/null 2>&1; then
	echo "CLI manifest validator accepted a Stable manifest as Preview" >&2
	exit 1
fi

# Desktop public pointers use the same strict channel grammar, but their assets
# must also share one exact immutable directory and complete integrity metadata.
desktop_compare="$repo_root/scripts/compare-desktop-release-versions.sh"
test -x "$desktop_compare"
[ "$(bash "$desktop_compare" stable v1.2.4 v1.2.3)" = "update" ]
[ "$(bash "$desktop_compare" stable v1.2.3 v1.2.3)" = "skip" ]
[ "$(bash "$desktop_compare" stable v1.2.2 v1.2.3)" = "skip" ]
[ "$(bash "$desktop_compare" stable v100000000000000000000.0.0 v99999999999999999999.999.999)" = "update" ]
[ "$(bash "$desktop_compare" preview v1.2.3-preview.11 v1.2.3-preview.9)" = "update" ]
[ "$(bash "$desktop_compare" preview v1.2.3-preview.9 v1.2.3-preview.9)" = "skip" ]
[ "$(bash "$desktop_compare" preview v1.2.3-preview.8 v1.2.3-preview.9)" = "skip" ]
[ "$(bash "$desktop_compare" preview v1.2.3-preview.1 "")" = "update" ]
if bash "$desktop_compare" stable v1.2.3 v1.2.3-preview.1 >/dev/null 2>&1; then
	echo "Desktop Stable comparator accepted a Preview pointer" >&2
	exit 1
fi
if bash "$desktop_compare" preview v1.2.3-preview.1 v1.2.3 >/dev/null 2>&1; then
	echo "Desktop Preview comparator accepted a Stable pointer" >&2
	exit 1
fi
if bash "$desktop_compare" preview v1.2.3-preview.01 v1.2.3-preview.1 >/dev/null 2>&1; then
	echo "Desktop comparator accepted a non-canonical Preview version" >&2
	exit 1
fi

desktop_pointer_decider="$repo_root/scripts/decide-desktop-pointer-update.sh"
test -x "$desktop_pointer_decider"
preview_candidate="$test_root/preview-candidate.json"
preview_older="$test_root/preview-older.json"
preview_equal="$test_root/preview-equal.json"
preview_equal_different="$test_root/preview-equal-different.json"
preview_newer="$test_root/preview-newer.json"
printf '{"version":"v1.3.0-preview.42","marker":"candidate"}\n' >"$preview_candidate"
printf '{"version":"v1.3.0-preview.41","marker":"older"}\n' >"$preview_older"
cp "$preview_candidate" "$preview_equal"
printf '{"version":"v1.3.0-preview.42","marker":"different"}\n' >"$preview_equal_different"
printf '{"version":"v1.3.0-preview.43","marker":"newer"}\n' >"$preview_newer"
preview_newer_equal="$test_root/preview-newer-equal.json"
preview_newer_conflict="$test_root/preview-newer-conflict.json"
cp "$preview_newer" "$preview_newer_equal"
printf '{"version":"v1.3.0-preview.43","marker":"conflict"}\n' >"$preview_newer_conflict"
expect_pointer_source() {
	local expected="$1"
	shift
	local decision
	local action
	local source
	decision="$(bash "$desktop_pointer_decider" "$@")"
	IFS=$'\t' read -r action source <<<"$decision"
	[ "$action" = "update" ]
	[ "$source" = "$expected" ]
}
expect_pointer_source "$preview_candidate" preview "$preview_candidate" - -
expect_pointer_source "$preview_candidate" preview "$preview_candidate" "$preview_older" "$preview_older"
expect_pointer_source "$preview_candidate" preview "$preview_candidate" "$preview_equal" "$preview_older"
expect_pointer_source "$preview_candidate" preview "$preview_candidate" "$preview_older" "$preview_equal"
[ "$(bash "$desktop_pointer_decider" preview "$preview_candidate" "$preview_equal" "$preview_equal")" = "skip" ]
expect_pointer_source "$preview_candidate" preview "$preview_candidate" "$preview_equal" "$preview_equal_different"
expect_pointer_source "$preview_newer" preview "$preview_candidate" "$preview_newer" "$preview_older"
[ "$(bash "$desktop_pointer_decider" preview "$preview_candidate" "$preview_newer" "$preview_newer_equal")" = "skip" ]
if bash "$desktop_pointer_decider" preview "$preview_candidate" "$preview_newer" \
	"$preview_newer_conflict" >/dev/null 2>&1; then
	echo "Desktop Preview pointer decider accepted conflicting equal newer aliases" >&2
	exit 1
fi
if bash "$desktop_pointer_decider" preview "$preview_candidate" "$test_root/missing.json" - >/dev/null 2>&1; then
	echo "Desktop Preview pointer decider accepted an unreadable manifest path" >&2
	exit 1
fi

stable_candidate="$test_root/stable-candidate.json"
stable_older="$test_root/stable-older.json"
stable_equal="$test_root/stable-equal.json"
stable_equal_different="$test_root/stable-equal-different.json"
stable_newer="$test_root/stable-newer.json"
printf '{"version":"v1.3.0","marker":"candidate"}\n' >"$stable_candidate"
printf '{"version":"v1.2.9","marker":"older"}\n' >"$stable_older"
cp "$stable_candidate" "$stable_equal"
printf '{"version":"v1.3.0","marker":"different"}\n' >"$stable_equal_different"
printf '{"version":"v1.3.1","marker":"newer"}\n' >"$stable_newer"
expect_pointer_source "$stable_candidate" stable "$stable_candidate" -
expect_pointer_source "$stable_candidate" stable "$stable_candidate" "$stable_older"
[ "$(bash "$desktop_pointer_decider" stable "$stable_candidate" "$stable_equal")" = "skip" ]
expect_pointer_source "$stable_candidate" stable "$stable_candidate" "$stable_equal_different"
[ "$(bash "$desktop_pointer_decider" stable "$stable_candidate" "$stable_newer")" = "skip" ]

desktop_directory_verifier="$repo_root/scripts/verify-desktop-release-directory.sh"
test -x "$desktop_directory_verifier"
candidate_directory="$test_root/desktop-directory-candidate"
existing_directory="$test_root/desktop-directory-existing"
mkdir -p "$candidate_directory" "$existing_directory"
printf 'asset-a\n' >"$candidate_directory/a"
printf 'asset-b\n' >"$candidate_directory/b"
cp "$candidate_directory/a" "$existing_directory/a"
bash "$desktop_directory_verifier" --allow-missing "$candidate_directory" "$existing_directory"
if bash "$desktop_directory_verifier" "$candidate_directory" "$existing_directory" >/dev/null 2>&1; then
	echo "Desktop release directory verifier accepted a missing object in exact mode" >&2
	exit 1
fi
cp "$candidate_directory/b" "$existing_directory/b"
bash "$desktop_directory_verifier" "$candidate_directory" "$existing_directory"
printf 'conflict\n' >"$existing_directory/a"
if bash "$desktop_directory_verifier" --allow-missing "$candidate_directory" "$existing_directory" >/dev/null 2>&1; then
	echo "Desktop release directory verifier accepted conflicting immutable content" >&2
	exit 1
fi
cp "$candidate_directory/a" "$existing_directory/a"
printf 'unexpected\n' >"$existing_directory/unexpected"
if bash "$desktop_directory_verifier" --allow-missing "$candidate_directory" "$existing_directory" >/dev/null 2>&1; then
	echo "Desktop release directory verifier accepted an unexpected immutable object" >&2
	exit 1
fi

legacy_candidate_directory="$test_root/desktop-directory-legacy-candidate"
legacy_existing_directory="$test_root/desktop-directory-legacy-existing"
mkdir -p "$legacy_candidate_directory" "$legacy_existing_directory"
printf 'asset\n' >"$legacy_candidate_directory/artifact"
cp "$legacy_candidate_directory/artifact" "$legacy_existing_directory/artifact"
jq -n '{
	version: "v1.2.3",
	release_notes_url: "https://reasonix.io/changelog/v1.2.3/",
	platforms: {"darwin-arm64": {size: 42}}
}' >"$legacy_candidate_directory/latest.json"
jq 'del(.release_notes_url)' "$legacy_candidate_directory/latest.json" \
	>"$legacy_existing_directory/latest.json"
bash "$desktop_directory_verifier" --allow-legacy-manifest \
	"$legacy_candidate_directory" "$legacy_existing_directory"
if bash "$desktop_directory_verifier" \
	"$legacy_candidate_directory" "$legacy_existing_directory" >/dev/null 2>&1; then
	echo "Desktop release directory verifier accepted a legacy manifest without opt-in" >&2
	exit 1
fi
jq '.platforms["darwin-arm64"].size = 99' "$legacy_existing_directory/latest.json" \
	>"$legacy_existing_directory/latest-conflict.json"
mv "$legacy_existing_directory/latest-conflict.json" "$legacy_existing_directory/latest.json"
if bash "$desktop_directory_verifier" --allow-legacy-manifest \
	"$legacy_candidate_directory" "$legacy_existing_directory" >/dev/null 2>&1; then
	echo "Desktop release directory verifier ignored a non-legacy manifest difference" >&2
	exit 1
fi

# GitHub release recovery uses the same immutable-subset rule: an interrupted
# release may fill missing assets, while conflicting or extra assets fail closed.
desktop_github_publisher="$repo_root/scripts/publish-desktop-github-release.sh"
test -x "$desktop_github_publisher"
fake_gh_bin="$test_root/fake-gh-bin"
fake_gh_state="$test_root/fake-gh-state"
mkdir -p "$fake_gh_bin" "$fake_gh_state"
cat >"$fake_gh_bin/gh" <<'FAKE_GH'
#!/usr/bin/env bash
set -euo pipefail
state="${FAKE_GH_STATE:?FAKE_GH_STATE is required}"
command="${1:-}"
shift || true

release_json() {
	if [ ! -f "$state/release.json" ]; then
		echo "HTTP 404: Not Found" >&2
		return 1
	fi
	assets='[]'
	if compgen -G "$state/assets/*" >/dev/null; then
		for asset in "$state/assets"/*; do
			assets="$(jq -cn --argjson current "$assets" --arg name "$(basename "$asset")" \
				'$current + [{name: $name}]')"
		done
	fi
	jq --argjson assets "$assets" '. + {assets: $assets}' "$state/release.json"
}

case "$command" in
api)
	release_json
	;;
release)
	subcommand="${1:-}"
	shift || true
	case "$subcommand" in
	create)
		tag="${1:?tag is required}"
		shift
		title=""
		notes_file=""
		prerelease=false
		while [ "$#" -gt 0 ]; do
			case "$1" in
			-R | --repo | --title | --notes-file)
				option="$1"
				value="${2:?$option requires a value}"
				shift 2
				case "$option" in
				--title) title="$value" ;;
				--notes-file) notes_file="$value" ;;
				esac
				;;
			--prerelease)
				prerelease=true
				shift
				;;
			--latest | --latest=false)
				shift
				;;
			*)
				echo "unexpected fake gh release create argument: $1" >&2
				exit 2
				;;
			esac
		done
		mkdir -p "$state/assets"
		jq -n --arg tag "$tag" --arg name "$title" --rawfile body "$notes_file" \
			--argjson prerelease "$prerelease" \
			'{tag_name: $tag, name: $name, body: $body, draft: false, prerelease: $prerelease}' \
			>"$state/release.json"
		;;
	download)
		tag="${1:?tag is required}"
		shift
		pattern=""
		destination=""
		while [ "$#" -gt 0 ]; do
			case "$1" in
			-R | --repo)
				shift 2
				;;
			-p | --pattern)
				pattern="${2:?pattern is required}"
				shift 2
				;;
			-D | --dir)
				destination="${2:?destination is required}"
				shift 2
				;;
			*)
				echo "unexpected fake gh release download argument: $1" >&2
				exit 2
				;;
			esac
		done
		test -n "$tag"
		mkdir -p "$destination"
		if [ -n "$pattern" ]; then
			cp "$state/assets/$pattern" "$destination/$pattern"
		elif compgen -G "$state/assets/*" >/dev/null; then
			cp "$state/assets"/* "$destination/"
		fi
		;;
	upload)
		tag="${1:?tag is required}"
		shift
		asset=""
		while [ "$#" -gt 0 ]; do
			case "$1" in
			-R | --repo)
				shift 2
				;;
			*)
				asset="$1"
				shift
				;;
			esac
		done
		test -n "$tag"
		mkdir -p "$state/assets"
		cp "$asset" "$state/assets/$(basename "$asset")"
		;;
	*)
		echo "unsupported fake gh command: $command $subcommand" >&2
		exit 2
	;;
	esac
	;;
*)
	echo "unsupported fake gh command: $command" >&2
	exit 2
	;;
esac
FAKE_GH
chmod +x "$fake_gh_bin/gh"

github_candidate="$test_root/desktop-github-candidate"
github_notes="$test_root/desktop-github-notes.md"
mkdir -p "$github_candidate"
printf 'asset-a\n' >"$github_candidate/a"
printf 'asset-b\n' >"$github_candidate/b"
printf 'Release notes.\n' >"$github_notes"
PATH="$fake_gh_bin:$PATH" FAKE_GH_STATE="$fake_gh_state" \
	GITHUB_REPOSITORY=esengine/DeepSeek-Reasonix \
	bash "$desktop_github_publisher" desktop-v1.2.3 v1.2.3 false \
	"$github_notes" "$github_candidate"
bash "$desktop_directory_verifier" "$github_candidate" "$fake_gh_state/assets"

rm -f "$fake_gh_state/assets/b"
PATH="$fake_gh_bin:$PATH" FAKE_GH_STATE="$fake_gh_state" \
	GITHUB_REPOSITORY=esengine/DeepSeek-Reasonix \
	bash "$desktop_github_publisher" desktop-v1.2.3 v1.2.3 false \
	"$github_notes" "$github_candidate"
bash "$desktop_directory_verifier" "$github_candidate" "$fake_gh_state/assets"

printf 'conflict\n' >"$fake_gh_state/assets/a"
if PATH="$fake_gh_bin:$PATH" FAKE_GH_STATE="$fake_gh_state" \
	GITHUB_REPOSITORY=esengine/DeepSeek-Reasonix \
	bash "$desktop_github_publisher" desktop-v1.2.3 v1.2.3 false \
	"$github_notes" "$github_candidate" >/dev/null 2>&1; then
	echo "Desktop GitHub recovery accepted conflicting immutable content" >&2
	exit 1
fi
cp "$github_candidate/a" "$fake_gh_state/assets/a"
printf 'unexpected\n' >"$fake_gh_state/assets/unexpected"
if PATH="$fake_gh_bin:$PATH" FAKE_GH_STATE="$fake_gh_state" \
	GITHUB_REPOSITORY=esengine/DeepSeek-Reasonix \
	bash "$desktop_github_publisher" desktop-v1.2.3 v1.2.3 false \
	"$github_notes" "$github_candidate" >/dev/null 2>&1; then
	echo "Desktop GitHub recovery accepted an unexpected immutable asset" >&2
	exit 1
fi
rm -f "$fake_gh_state/assets/unexpected"
jq '.name = "Wrong title"' "$fake_gh_state/release.json" >"$fake_gh_state/release.json.new"
mv "$fake_gh_state/release.json.new" "$fake_gh_state/release.json"
if PATH="$fake_gh_bin:$PATH" FAKE_GH_STATE="$fake_gh_state" \
	GITHUB_REPOSITORY=esengine/DeepSeek-Reasonix \
	bash "$desktop_github_publisher" desktop-v1.2.3 v1.2.3 false \
	"$github_notes" "$github_candidate" >/dev/null 2>&1; then
	echo "Desktop GitHub recovery accepted conflicting release metadata" >&2
	exit 1
fi

desktop_validator="$repo_root/scripts/validate-desktop-release-manifest.sh"
desktop_manifest_comparator="$repo_root/scripts/compare-desktop-release-manifests.sh"
test -x "$desktop_validator"
test -x "$desktop_manifest_comparator"
write_desktop_manifest() {
	local version="$1"
	local base="$2"
	local output="$3"
	local notes_version="${4:-$version}"
	jq -n --arg version "$version" --arg notes_version "$notes_version" \
		--arg base "$base" --arg sha "$(printf 'a%.0s' {1..64})" '
		def asset($name): {
			url: ($base + $name),
			sig: ($base + $name + ".minisig"),
			size: 42,
			sha256: $sha
		};
		{
			version: $version,
			download_page: "https://reasonix.io/?download=desktop#start",
			release_notes_url: ("https://reasonix.io/changelog/" + $notes_version + "/"),
			platforms: {
				"darwin-arm64": asset("Reasonix-darwin-arm64.zip"),
				"darwin-amd64": asset("Reasonix-darwin-amd64.zip"),
				"windows-amd64": asset("Reasonix-windows-amd64-installer.exe"),
				"windows-arm64": asset("Reasonix-windows-arm64-installer.exe"),
				"linux-amd64": asset("Reasonix-linux-amd64.tar.gz")
			},
			native_packages: {
				"linux-amd64": asset("Reasonix-linux-amd64.deb")
			},
			downloads: {
				"Reasonix-darwin-universal.dmg": asset("Reasonix-darwin-universal.dmg"),
				"Reasonix-windows-amd64.zip": asset("Reasonix-windows-amd64.zip")
			}
		}
	' >"$output"
}

desktop_stable_version="v1.2.3"
desktop_stable_base="https://dl.reasonix.io/desktop-${desktop_stable_version}/"
desktop_stable_manifest="$test_root/desktop-stable.json"
write_desktop_manifest "$desktop_stable_version" "$desktop_stable_base" "$desktop_stable_manifest"
bash "$desktop_validator" stable "$desktop_stable_version" "$desktop_stable_base" "$desktop_stable_manifest"

desktop_github_base="https://github.com/esengine/DeepSeek-Reasonix/releases/download/desktop-${desktop_stable_version}/"
desktop_github_manifest="$test_root/desktop-stable-github.json"
write_desktop_manifest "$desktop_stable_version" "$desktop_github_base" "$desktop_github_manifest"
bash "$desktop_validator" stable "$desktop_stable_version" "$desktop_github_base" "$desktop_github_manifest"

desktop_preview_version="v1.3.0-preview.42"
desktop_preview_base="https://dl.reasonix.io/desktop-${desktop_preview_version}/"
desktop_preview_manifest="$test_root/desktop-preview.json"
write_desktop_manifest "$desktop_preview_version" "$desktop_preview_base" "$desktop_preview_manifest"
bash "$desktop_validator" preview "$desktop_preview_version" "$desktop_preview_base" "$desktop_preview_manifest"

desktop_rc_version="v1.3.0-rc.1"
desktop_rc_notes_version="v1.3.0"
desktop_rc_base="https://dl.reasonix.io/desktop-${desktop_rc_version}/"
desktop_rc_manifest="$test_root/desktop-rc.json"
write_desktop_manifest "$desktop_rc_version" "$desktop_rc_base" "$desktop_rc_manifest" \
	"$desktop_rc_notes_version"
bash "$desktop_validator" any "$desktop_rc_version" "$desktop_rc_base" \
	"$desktop_rc_manifest" "$desktop_rc_notes_version"

desktop_legacy_notes_manifest="$test_root/desktop-stable-legacy-notes.json"
jq 'del(.release_notes_url)' "$desktop_stable_manifest" >"$desktop_legacy_notes_manifest"
bash "$desktop_validator" legacy-stable "$desktop_stable_version" \
	"$desktop_stable_base" "$desktop_legacy_notes_manifest"
bash "$desktop_manifest_comparator" "$desktop_stable_manifest" "$desktop_legacy_notes_manifest"
if bash "$desktop_validator" stable "$desktop_stable_version" "$desktop_stable_base" \
	"$desktop_legacy_notes_manifest" >/dev/null 2>&1; then
	echo "strict Desktop manifest validator accepted a legacy release-notes manifest" >&2
	exit 1
fi
jq '.release_notes_url = "https://reasonix.io/changelog/v9.9.9/"' \
	"$desktop_stable_manifest" >"$test_root/desktop-wrong-legacy-notes.json"
if bash "$desktop_manifest_comparator" "$desktop_stable_manifest" \
	"$test_root/desktop-wrong-legacy-notes.json" >/dev/null 2>&1; then
	echo "Desktop manifest comparator ignored a non-legacy difference" >&2
	exit 1
fi

expect_invalid_desktop_manifest() {
	local description="$1"
	local channel="$2"
	local version="$3"
	local base="$4"
	local file="$5"
	if bash "$desktop_validator" "$channel" "$version" "$base" "$file" >/dev/null 2>&1; then
		echo "Desktop manifest validator accepted $description" >&2
		exit 1
	fi
}

jq 'del(.platforms["windows-arm64"])' "$desktop_preview_manifest" >"$test_root/desktop-missing-platform.json"
expect_invalid_desktop_manifest "a missing platform" preview "$desktop_preview_version" \
	"$desktop_preview_base" "$test_root/desktop-missing-platform.json"
jq '.platforms["darwin-arm64"].url = "https://evil.invalid/file"' \
	"$desktop_preview_manifest" >"$test_root/desktop-hostile-url.json"
expect_invalid_desktop_manifest "a hostile URL" preview "$desktop_preview_version" \
	"$desktop_preview_base" "$test_root/desktop-hostile-url.json"
jq '.platforms["darwin-arm64"].sig += "?mirror=1"' \
	"$desktop_preview_manifest" >"$test_root/desktop-bad-signature.json"
expect_invalid_desktop_manifest "a mismatched signature URL" preview "$desktop_preview_version" \
	"$desktop_preview_base" "$test_root/desktop-bad-signature.json"
jq '.native_packages["linux-amd64"].size = 0' \
	"$desktop_preview_manifest" >"$test_root/desktop-zero-size.json"
expect_invalid_desktop_manifest "a zero-byte native package" preview "$desktop_preview_version" \
	"$desktop_preview_base" "$test_root/desktop-zero-size.json"
jq '.native_packages["linux-amd64"].size = 1073741825' \
	"$desktop_preview_manifest" >"$test_root/desktop-oversized-asset.json"
expect_invalid_desktop_manifest "an asset above the release size limit" preview \
	"$desktop_preview_version" "$desktop_preview_base" \
	"$test_root/desktop-oversized-asset.json"
jq '.native_packages["linux-amd64"].size = 9007199254740992' \
	"$desktop_preview_manifest" >"$test_root/desktop-unsafe-integer-size.json"
expect_invalid_desktop_manifest "an asset size above the JavaScript safe-integer range" preview \
	"$desktop_preview_version" "$desktop_preview_base" \
	"$test_root/desktop-unsafe-integer-size.json"
jq '.downloads["Reasonix-darwin-universal.dmg"].sha256 = ("A" * 64)' \
	"$desktop_preview_manifest" >"$test_root/desktop-bad-sha.json"
expect_invalid_desktop_manifest "an uppercase SHA-256" preview "$desktop_preview_version" \
	"$desktop_preview_base" "$test_root/desktop-bad-sha.json"
jq 'del(.downloads["Reasonix-windows-amd64.zip"])' \
	"$desktop_preview_manifest" >"$test_root/desktop-missing-download.json"
expect_invalid_desktop_manifest "a missing website download" preview "$desktop_preview_version" \
	"$desktop_preview_base" "$test_root/desktop-missing-download.json"
jq '.platforms.extra = .platforms["darwin-arm64"]' \
	"$desktop_preview_manifest" >"$test_root/desktop-extra-platform.json"
expect_invalid_desktop_manifest "an unexpected platform" preview "$desktop_preview_version" \
	"$desktop_preview_base" "$test_root/desktop-extra-platform.json"
write_desktop_manifest "$desktop_preview_version" "https://dl.reasonix.io/desktop-preview/" \
	"$test_root/desktop-rolling-preview.json"
expect_invalid_desktop_manifest "the legacy mutable Preview directory" preview "$desktop_preview_version" \
	"$desktop_preview_base" "$test_root/desktop-rolling-preview.json"
jq 'del(.downloads)' "$test_root/desktop-rolling-preview.json" \
	>"$test_root/desktop-legacy-preview.json"
bash "$desktop_validator" legacy-preview "$desktop_preview_version" \
	"https://dl.reasonix.io/desktop-preview/" "$test_root/desktop-legacy-preview.json"
jq 'del(.downloads)' "$desktop_stable_manifest" >"$test_root/desktop-legacy-stable.json"
bash "$desktop_validator" legacy-stable "$desktop_stable_version" \
	"$desktop_stable_base" "$test_root/desktop-legacy-stable.json"
jq '.downloads = null' "$desktop_stable_manifest" >"$test_root/desktop-null-legacy-stable.json"
bash "$desktop_validator" legacy-stable "$desktop_stable_version" \
	"$desktop_stable_base" "$test_root/desktop-null-legacy-stable.json"
jq '.downloads = {}' "$desktop_stable_manifest" >"$test_root/desktop-empty-downloads.json"
expect_invalid_desktop_manifest "empty downloads in a legacy Stable manifest" legacy-stable \
	"$desktop_stable_version" "$desktop_stable_base" "$test_root/desktop-empty-downloads.json"
expect_invalid_desktop_manifest "a legacy Stable manifest as a new publication" stable \
	"$desktop_stable_version" "$desktop_stable_base" "$test_root/desktop-legacy-stable.json"
jq 'del(.downloads["Reasonix-windows-amd64.zip"])' \
	"$test_root/desktop-rolling-preview.json" >"$test_root/desktop-partial-legacy-downloads.json"
expect_invalid_desktop_manifest "partial downloads in a legacy Preview manifest" legacy-preview \
	"$desktop_preview_version" "https://dl.reasonix.io/desktop-preview/" \
	"$test_root/desktop-partial-legacy-downloads.json"
expect_invalid_desktop_manifest "a Preview manifest as Stable" stable "$desktop_preview_version" \
	"$desktop_preview_base" "$desktop_preview_manifest"
expect_invalid_desktop_manifest "a non-official asset base" preview "$desktop_preview_version" \
	"https://cdn.invalid/desktop-${desktop_preview_version}/" "$desktop_preview_manifest"

# Release notes should normally reuse an existing release-bound PR instead of
# opening one PR per version. Fork PRs and stale branches must fail closed, and
# the dedicated release-notes PR remains the explicit fallback.
prepare_notes="$repo_root/.github/workflows/prepare-release-notes.yml"
grep -Eq '^      target_pr:$' "$prepare_notes"
grep -Eq 'isCrossRepository' "$prepare_notes"
grep -Eq 'target_pr comes from a fork' "$prepare_notes"
grep -Eq 'git merge-base --is-ancestor origin/main-v2 HEAD' "$prepare_notes"
grep -Eq 'RELEASE_NOTES_PR=\$TARGET_PR' "$prepare_notes"
grep -Eq 'Updated existing release-bound PR' "$prepare_notes"
grep -Eq 'release-notes/v\$\{VERSION#v\}' "$prepare_notes"
grep -Eq 'GITHUB_STEP_SUMMARY' "$prepare_notes"

desktop_candidate_resolver="$repo_root/scripts/resolve-desktop-candidate.sh"
test -x "$desktop_candidate_resolver"

git init --bare -q "$test_root/remote.git"
git clone -q "$test_root/remote.git" "$test_root/repo"
(
	cd "$test_root/repo"
	git config user.name "Release Workflow Test"
	git config user.email "release-workflow-test@example.invalid"
	git commit --allow-empty -q -m "candidate"
	git branch -M main-v2
	git push -q -u origin main-v2

	git tag v1.2.3
	git tag npm-v1.2.3
	git tag -a desktop-v1.2.3 -m "desktop release"
	git tag v1.3.0-preview.42
	git push -q origin v1.2.3 npm-v1.2.3 desktop-v1.2.3 v1.3.0-preview.42
	GITHUB_OUTPUT="$test_root/stable.out" RELEASE_TAG=v1.2.3 "$repo_root/scripts/resolve-stable-release.sh"
	grep -Eq '^version=1\.2\.3$' "$test_root/stable.out"
	grep -Eq '^desktop_tag=desktop-v1\.2\.3$' "$test_root/stable.out"
	GITHUB_OUTPUT="$test_root/preview.out" RELEASE_TAG=v1.3.0-preview.42 \
		"$repo_root/scripts/resolve-preview-release.sh"
	grep -Eq '^version=1\.3\.0-preview\.42$' "$test_root/preview.out"
	grep -Eq '^desktop_tag=desktop-v1\.3\.0-preview\.42$' "$test_root/preview.out"
	grep -Eq '^npm_version=1\.3\.0-canary\.42$' "$test_root/preview.out"
	approved_sha="$(git rev-parse HEAD)"
	GITHUB_OUTPUT="$test_root/desktop-stable-candidate.out" \
		RELEASE_CHANNEL=stable RELEASE_TAG=desktop-v1.2.3 \
		IN_ORCHESTRATED=false CALLER_EVENT_NAME=workflow_dispatch \
		CALLER_REF=refs/heads/main-v2 CALLER_REF_PROTECTED=true \
		CALLER_SHA="$approved_sha" CALLER_WORKFLOW_SHA="$approved_sha" \
		"$desktop_candidate_resolver"
	grep -Eq '^sha='"$approved_sha"'$' "$test_root/desktop-stable-candidate.out"
	GITHUB_OUTPUT="$test_root/desktop-preview-candidate.out" \
		RELEASE_CHANNEL=preview RELEASE_TAG=desktop-v1.3.0-preview.42 \
		IN_ORCHESTRATED=false CALLER_EVENT_NAME=workflow_dispatch \
		CALLER_REF=refs/heads/main-v2 CALLER_REF_PROTECTED=true \
		CALLER_SHA="$approved_sha" CALLER_WORKFLOW_SHA="$approved_sha" \
		"$desktop_candidate_resolver"
	grep -Eq '^sha='"$approved_sha"'$' "$test_root/desktop-preview-candidate.out"
	if GITHUB_OUTPUT="$test_root/desktop-unprotected-candidate.out" \
		RELEASE_CHANNEL=preview RELEASE_TAG=desktop-v1.3.0-preview.42 \
		IN_ORCHESTRATED=false CALLER_EVENT_NAME=workflow_dispatch \
		CALLER_REF=refs/heads/topic CALLER_REF_PROTECTED=false \
		CALLER_SHA="$approved_sha" CALLER_WORKFLOW_SHA="$approved_sha" \
		"$desktop_candidate_resolver" >"$test_root/desktop-unprotected-candidate.log" 2>&1; then
		echo "unprotected Desktop candidate unexpectedly passed" >&2
		exit 1
	fi
	grep -Eq 'standalone Desktop releases must run from protected main-v2' \
		"$test_root/desktop-unprotected-candidate.log"
	git tag desktop-v1.3.0-preview.42
	if GITHUB_OUTPUT="$test_root/desktop-tagged-preview-candidate.out" \
		RELEASE_CHANNEL=preview RELEASE_TAG=desktop-v1.3.0-preview.42 \
		IN_ORCHESTRATED=false CALLER_EVENT_NAME=workflow_dispatch \
		CALLER_REF=refs/heads/main-v2 CALLER_REF_PROTECTED=true \
		CALLER_SHA="$approved_sha" CALLER_WORKFLOW_SHA="$approved_sha" \
		"$desktop_candidate_resolver" >"$test_root/desktop-tagged-preview-candidate.log" 2>&1; then
		echo "Git-tagged Desktop Preview candidate unexpectedly passed" >&2
		exit 1
	fi
	grep -Eq 'Desktop Preview uses an immutable asset directory, not a Git tag' \
		"$test_root/desktop-tagged-preview-candidate.log"
	git tag -d desktop-v1.3.0-preview.42 >/dev/null

	ACTUAL_CALLER_WORKFLOW_REF='example/reasonix/.github/workflows/release-stable.yml@refs/tags/v1.2.3' \
		EXPECTED_CALLER_WORKFLOW_REF='example/reasonix/.github/workflows/release-stable.yml@refs/tags/v1.2.3' \
		CALLER_EVENT_NAME=push CALLER_REF=refs/tags/v1.2.3 CALLER_REF_PROTECTED=true \
		CALLER_WORKFLOW_SHA="$approved_sha" CALLER_SHA="$approved_sha" \
		APPROVED_CLI_TAG=v1.2.3 APPROVED_SHA="$approved_sha" \
		"$repo_root/scripts/verify-release-authorization.sh"
	ACTUAL_CALLER_WORKFLOW_REF='example/reasonix/.github/workflows/release-preview.yml@refs/heads/main-v2' \
		EXPECTED_CALLER_WORKFLOW_REF='example/reasonix/.github/workflows/release-preview.yml@refs/heads/main-v2' \
		CALLER_EVENT_NAME=workflow_dispatch CALLER_REF=refs/heads/main-v2 CALLER_REF_PROTECTED=true \
		CALLER_WORKFLOW_SHA="$approved_sha" CALLER_SHA="$approved_sha" \
		APPROVED_CHANNEL=preview APPROVED_CLI_TAG=v1.3.0-preview.42 APPROVED_SHA="$approved_sha" \
		"$repo_root/scripts/verify-release-authorization.sh"
	RELEASE_TAG=desktop-v1.2.3 APPROVED_SHA="$approved_sha" \
		"$repo_root/scripts/verify-release-tag.sh"

	git commit --allow-empty -q -m "release workflow fix"
	git push -q origin main-v2
	recovery_workflow_sha="$(git rev-parse HEAD)"
	if GITHUB_OUTPUT="$test_root/desktop-stale-preview-candidate.out" \
		RELEASE_CHANNEL=preview RELEASE_TAG=desktop-v1.3.0-preview.42 \
		IN_ORCHESTRATED=false CALLER_EVENT_NAME=workflow_dispatch \
		CALLER_REF=refs/heads/main-v2 CALLER_REF_PROTECTED=true \
		CALLER_SHA="$approved_sha" CALLER_WORKFLOW_SHA="$approved_sha" \
		"$desktop_candidate_resolver" >"$test_root/desktop-stale-preview-candidate.log" 2>&1; then
		echo "stale Desktop Preview candidate unexpectedly passed" >&2
		exit 1
	fi
	grep -Eq 'Desktop Preview must use current main-v2' \
		"$test_root/desktop-stale-preview-candidate.log"
	GITHUB_OUTPUT="$test_root/desktop-stable-recovery-candidate.out" \
		RELEASE_CHANNEL=stable RELEASE_TAG=desktop-v1.2.3 \
		IN_ORCHESTRATED=false CALLER_EVENT_NAME=workflow_dispatch \
		CALLER_REF=refs/heads/main-v2 CALLER_REF_PROTECTED=true \
		CALLER_SHA="$recovery_workflow_sha" CALLER_WORKFLOW_SHA="$recovery_workflow_sha" \
		"$desktop_candidate_resolver"
	grep -Eq '^sha='"$approved_sha"'$' "$test_root/desktop-stable-recovery-candidate.out"
	if RELEASE_TAG=v1.2.3 "$repo_root/scripts/resolve-stable-release.sh" >"$test_root/stale-main.log" 2>&1; then
		echo "old stable tag unexpectedly passed normal release resolution" >&2
		exit 1
	fi
	if ! grep -Eq 'points to .*expected' "$test_root/stale-main.log"; then
		sed -n '1,20p' "$test_root/stale-main.log" >&2
		exit 1
	fi
	GITHUB_OUTPUT="$test_root/recovery.out" ALLOW_STABLE_RECOVERY=true RELEASE_TAG=v1.2.3 \
		"$repo_root/scripts/resolve-stable-release.sh"
	grep -Eq '^sha='"$approved_sha"'$' "$test_root/recovery.out"
	ACTUAL_CALLER_WORKFLOW_REF='example/reasonix/.github/workflows/release-stable.yml@refs/heads/main-v2' \
		EXPECTED_CALLER_WORKFLOW_REF='example/reasonix/.github/workflows/release-stable.yml@refs/heads/main-v2' \
		CALLER_EVENT_NAME=workflow_dispatch CALLER_REF=refs/heads/main-v2 CALLER_REF_PROTECTED=true \
		CALLER_WORKFLOW_SHA="$recovery_workflow_sha" CALLER_SHA="$recovery_workflow_sha" \
		APPROVED_CLI_TAG=v1.2.3 APPROVED_SHA="$approved_sha" \
		"$repo_root/scripts/verify-release-authorization.sh"
	RELEASE_TAG=v1.2.3 APPROVED_SHA="$approved_sha" VERIFY_RELEASE_CHECKOUT=false \
		"$repo_root/scripts/verify-release-tag.sh"
	if ACTUAL_CALLER_WORKFLOW_REF='example/reasonix/.github/workflows/release-stable.yml@refs/heads/main-v2' \
		EXPECTED_CALLER_WORKFLOW_REF='example/reasonix/.github/workflows/release-stable.yml@refs/heads/main-v2' \
		CALLER_EVENT_NAME=workflow_dispatch CALLER_REF=refs/heads/main-v2 CALLER_REF_PROTECTED=true \
		CALLER_WORKFLOW_SHA="$approved_sha" CALLER_SHA="$recovery_workflow_sha" \
		APPROVED_CLI_TAG=v1.2.3 APPROVED_SHA="$approved_sha" \
		"$repo_root/scripts/verify-release-authorization.sh" >"$test_root/stale-workflow.log" 2>&1; then
		echo "stale recovery workflow unexpectedly passed release authorization" >&2
		exit 1
	fi
	grep -Eq 'recovery caller workflow SHA is' "$test_root/stale-workflow.log"

	if ACTUAL_CALLER_WORKFLOW_REF='example/reasonix/.github/workflows/release-stable.yml@refs/heads/topic' \
		EXPECTED_CALLER_WORKFLOW_REF='example/reasonix/.github/workflows/release-stable.yml@refs/heads/topic' \
		CALLER_EVENT_NAME=workflow_dispatch CALLER_REF=refs/heads/topic CALLER_REF_PROTECTED=false \
		CALLER_WORKFLOW_SHA="$recovery_workflow_sha" CALLER_SHA="$recovery_workflow_sha" \
		APPROVED_CLI_TAG=v1.2.3 APPROVED_SHA="$approved_sha" \
		"$repo_root/scripts/verify-release-authorization.sh" >"$test_root/unprotected.log" 2>&1; then
		echo "unprotected caller unexpectedly passed release authorization" >&2
		exit 1
	fi
	grep -Eq 'caller ref is not protected' "$test_root/unprotected.log"

	git tag v1.2.4
	git tag npm-v1.2.4
	git push -q origin v1.2.4 npm-v1.2.4
	if RELEASE_TAG=v1.2.4 "$repo_root/scripts/resolve-stable-release.sh" >"$test_root/missing.log" 2>&1; then
		echo "missing sibling tag unexpectedly passed" >&2
		exit 1
	fi
	grep -Eq 'required stable release tag is missing: desktop-v1\.2\.4' "$test_root/missing.log"

	other_sha="$(git commit-tree HEAD^{tree} -p HEAD -m "other")"
	git tag v1.2.6 "$other_sha"
	git tag npm-v1.2.6 "$other_sha"
	git tag desktop-v1.2.6 "$other_sha"
	git push -q origin v1.2.6 npm-v1.2.6 desktop-v1.2.6
	if ALLOW_STABLE_RECOVERY=true RELEASE_TAG=v1.2.6 \
		"$repo_root/scripts/resolve-stable-release.sh" >"$test_root/non-ancestor.log" 2>&1; then
		echo "non-ancestor recovery tags unexpectedly passed release resolution" >&2
		exit 1
	fi
	grep -Eq 'is not an ancestor of' "$test_root/non-ancestor.log"
	git tag -f desktop-v1.2.3 "$other_sha" >/dev/null
	git push -q -f origin desktop-v1.2.3
	git checkout -q --detach "$approved_sha"
	if RELEASE_TAG=desktop-v1.2.3 APPROVED_SHA="$approved_sha" \
		"$repo_root/scripts/verify-release-tag.sh" >"$test_root/moved-tag.log" 2>&1; then
		echo "moved release tag unexpectedly passed approved SHA validation" >&2
		exit 1
	fi
	grep -Eq 'moved to .* after approval' "$test_root/moved-tag.log"
	git tag -f desktop-v1.2.3 "$approved_sha" >/dev/null
	git push -q -f origin desktop-v1.2.3
	git switch -q main-v2

	git tag v1.2.5
	git tag npm-v1.2.5 "$other_sha"
	git tag desktop-v1.2.5
	git push -q origin v1.2.5 npm-v1.2.5 desktop-v1.2.5
	if RELEASE_TAG=v1.2.5 "$repo_root/scripts/resolve-stable-release.sh" >"$test_root/mismatch.log" 2>&1; then
		echo "mismatched sibling tag unexpectedly passed" >&2
		exit 1
	fi
	grep -Eq 'npm-v1\.2\.5 points to .* expected' "$test_root/mismatch.log"

	if RELEASE_TAG=v1.2.3-rc.1 "$repo_root/scripts/resolve-stable-release.sh" >"$test_root/prerelease.log" 2>&1; then
		echo "prerelease tag unexpectedly passed stable validation" >&2
		exit 1
	fi
	grep -Eq 'stable release tag must be vMAJOR.MINOR.PATCH' "$test_root/prerelease.log"
)

EVENT_NAME=push IN_CHANNEL=stable IN_TAG=desktop-v1.2.3 REF_NAME=v1.2.3 RUN_NUMBER=10 \
	GITHUB_OUTPUT="$test_root/desktop-stable.out" bash "$repo_root/scripts/resolve-desktop-release.sh"
grep -Eq '^tag=desktop-v1\.2\.3$' "$test_root/desktop-stable.out"
grep -Eq '^version=v1\.2\.3$' "$test_root/desktop-stable.out"
grep -Eq '^notes_version=v1\.2\.3$' "$test_root/desktop-stable.out"

EVENT_NAME=workflow_call IN_ORCHESTRATED=true IN_CHANNEL=preview IN_BASE_VERSION=1.3.0 \
	IN_TAG='' REF_NAME=main-v2 RUN_NUMBER=999 IN_PREVIEW_NUMBER=42 \
	GITHUB_OUTPUT="$test_root/desktop-preview.out" bash "$repo_root/scripts/resolve-desktop-release.sh"
grep -Eq '^version=v1\.3\.0-preview\.42$' "$test_root/desktop-preview.out"
grep -Eq '^tag=desktop-v1\.3\.0-preview\.42$' "$test_root/desktop-preview.out"
grep -Eq '^channel=preview$' "$test_root/desktop-preview.out"
grep -Eq '^notes_version=v1\.3\.0-preview\.42$' "$test_root/desktop-preview.out"

if EVENT_NAME=workflow_call IN_ORCHESTRATED=true IN_CHANNEL=preview IN_BASE_VERSION=1.3.0 \
	IN_TAG=desktop-v1.3.0-preview.42 REF_NAME=main-v2 RUN_NUMBER=42 \
	GITHUB_OUTPUT="$test_root/desktop-preview-tagged.out" \
	bash "$repo_root/scripts/resolve-desktop-release.sh" \
	>"$test_root/desktop-preview-tagged.log" 2>&1; then
	echo "tagged Desktop Preview dispatch unexpectedly passed" >&2
	exit 1
fi
grep -Eq 'Desktop Preview versions are synthesized from protected main-v2' \
	"$test_root/desktop-preview-tagged.log"

# Legacy signing automation may still dispatch `canary`; normalize it to Preview
# without exposing a third public release path.
EVENT_NAME=workflow_dispatch IN_ORCHESTRATED=false IN_SIGNING_PREFLIGHT=true \
	IN_CHANNEL=canary IN_BASE_VERSION=1.3.0 IN_TAG='' REF_NAME=main-v2 RUN_NUMBER=43 \
	GITHUB_OUTPUT="$test_root/desktop-canary-compat.out" bash "$repo_root/scripts/resolve-desktop-release.sh"
grep -Eq '^version=v1\.3\.0-preview\.43$' "$test_root/desktop-canary-compat.out"
grep -Eq '^tag=desktop-v1\.3\.0-preview\.43$' "$test_root/desktop-canary-compat.out"
grep -Eq '^channel=preview$' "$test_root/desktop-canary-compat.out"

EVENT_NAME=workflow_dispatch IN_ORCHESTRATED=false IN_PRODUCTION_SIGNING_SMOKE=true \
	IN_CHANNEL=preview IN_BASE_VERSION=1.3.0 IN_TAG='' REF_NAME=main-v2 RUN_NUMBER=44 \
	GITHUB_OUTPUT="$test_root/desktop-preview-smoke.out" bash "$repo_root/scripts/resolve-desktop-release.sh"
grep -Eq '^version=v1\.3\.0-preview\.44$' "$test_root/desktop-preview-smoke.out"

if EVENT_NAME=workflow_dispatch IN_ORCHESTRATED=false IN_CHANNEL=preview \
	IN_BASE_VERSION=1.3.0 IN_TAG='' REF_NAME=main-v2 RUN_NUMBER=45 \
	GITHUB_OUTPUT="$test_root/desktop-preview-direct.out" \
	bash "$repo_root/scripts/resolve-desktop-release.sh" \
	>"$test_root/desktop-preview-direct.log" 2>&1; then
	echo "standalone public Desktop Preview unexpectedly passed" >&2
	exit 1
fi
grep -Eq 'public Desktop Preview releases must be dispatched by release-preview.yml' \
	"$test_root/desktop-preview-direct.log"

EVENT_NAME=push IN_CHANNEL='' IN_TAG='' REF_NAME=desktop-v1.4.0-rc.1 RUN_NUMBER=50 \
	GITHUB_OUTPUT="$test_root/desktop-rc.out" bash "$repo_root/scripts/resolve-desktop-release.sh"
grep -Eq '^prerelease=true$' "$test_root/desktop-rc.out"
grep -Eq '^notes_version=v1\.4\.0$' "$test_root/desktop-rc.out"

if EVENT_NAME=workflow_dispatch IN_CHANNEL=stable IN_TAG=desktop-v1.4.0-preview.1 \
	REF_NAME=main-v2 RUN_NUMBER=50 GITHUB_OUTPUT="$test_root/desktop-preview-as-stable.out" \
	bash "$repo_root/scripts/resolve-desktop-release.sh" \
	>"$test_root/desktop-preview-as-stable.log" 2>&1; then
	echo "Desktop Preview tag unexpectedly passed as Stable" >&2
	exit 1
fi
grep -Eq 'Desktop Preview versions must use channel=preview without a Git tag' \
	"$test_root/desktop-preview-as-stable.log"

if EVENT_NAME=workflow_dispatch IN_CHANNEL=stable IN_TAG='' REF_NAME=main-v2 RUN_NUMBER=50 \
	GITHUB_OUTPUT="$test_root/desktop-missing-tag.out" bash "$repo_root/scripts/resolve-desktop-release.sh" \
	>"$test_root/desktop-missing-tag.log" 2>&1; then
	echo "tag-less desktop stable dispatch unexpectedly passed" >&2
	exit 1
fi
grep -Eq 'stable dispatch requires tag' "$test_root/desktop-missing-tag.log"

EVENT_NAME=workflow_call IN_ORCHESTRATED=true IN_CHANNEL=preview IN_TAG=v1.3.0-preview.42 \
	REF_NAME=main-v2 GITHUB_OUTPUT="$test_root/cli-preview-call.out" \
	bash "$repo_root/scripts/resolve-cli-release.sh"
grep -Eq '^tag=v1\.3\.0-preview\.42$' "$test_root/cli-preview-call.out"
grep -Eq '^version=1\.3\.0-preview\.42$' "$test_root/cli-preview-call.out"
grep -Eq '^base_version=1\.3\.0$' "$test_root/cli-preview-call.out"
grep -Eq '^notes_version=v1\.3\.0-preview\.42$' "$test_root/cli-preview-call.out"
grep -Eq '^channel=preview$' "$test_root/cli-preview-call.out"
grep -Eq '^prerelease=true$' "$test_root/cli-preview-call.out"

if EVENT_NAME=workflow_dispatch IN_ORCHESTRATED=false IN_CHANNEL=preview \
	IN_TAG=v1.3.0-preview.42 REF_NAME=main-v2 CALLER_REF=refs/heads/main-v2 \
	CALLER_REF_PROTECTED=true GITHUB_OUTPUT="$test_root/cli-preview-dispatch.out" \
	bash "$repo_root/scripts/resolve-cli-release.sh" \
	>"$test_root/cli-preview-dispatch.log" 2>&1; then
	echo "standalone public CLI Preview unexpectedly passed" >&2
	exit 1
fi
grep -Eq 'public CLI Preview releases must be dispatched by release-preview.yml' \
	"$test_root/cli-preview-dispatch.log"

EVENT_NAME=push IN_CHANNEL='' IN_TAG='' REF_NAME=v1.3.0-rc.1 \
	GITHUB_OUTPUT="$test_root/cli-rc.out" bash "$repo_root/scripts/resolve-cli-release.sh"
grep -Eq '^channel=stable$' "$test_root/cli-rc.out"
grep -Eq '^prerelease=true$' "$test_root/cli-rc.out"
grep -Eq '^notes_version=v1\.3\.0$' "$test_root/cli-rc.out"

if EVENT_NAME=workflow_dispatch IN_ORCHESTRATED=false IN_CHANNEL=stable \
	IN_TAG=v1.3.0-preview.42 REF_NAME=main-v2 CALLER_REF=refs/heads/main-v2 \
	CALLER_REF_PROTECTED=true GITHUB_OUTPUT="$test_root/cli-channel-mismatch.out" \
	bash "$repo_root/scripts/resolve-cli-release.sh" >"$test_root/cli-channel-mismatch.log" 2>&1; then
	echo "CLI Preview tag unexpectedly passed as Stable" >&2
	exit 1
fi
grep -Eq 'belongs to preview, not requested channel stable' "$test_root/cli-channel-mismatch.log"

if EVENT_NAME=push IN_CHANNEL='' IN_TAG='' REF_NAME=v1.3.0-preview.latest \
	GITHUB_OUTPUT="$test_root/cli-invalid-preview.out" bash "$repo_root/scripts/resolve-cli-release.sh" \
	>"$test_root/cli-invalid-preview.log" 2>&1; then
	echo "malformed CLI Preview tag unexpectedly passed" >&2
	exit 1
fi
grep -Eq 'CLI Preview tag must be vMAJOR.MINOR.PATCH-preview.N' "$test_root/cli-invalid-preview.log"

if EVENT_NAME=workflow_dispatch IN_ORCHESTRATED=false IN_CHANNEL=preview \
	IN_TAG=v1.3.0-preview.42 REF_NAME=topic CALLER_REF=refs/heads/topic \
	CALLER_REF_PROTECTED=false GITHUB_OUTPUT="$test_root/cli-unprotected.out" \
	bash "$repo_root/scripts/resolve-cli-release.sh" >"$test_root/cli-unprotected.log" 2>&1; then
	echo "unprotected CLI Preview dispatch unexpectedly passed" >&2
	exit 1
fi
grep -Eq 'manual CLI releases must run from protected main-v2' "$test_root/cli-unprotected.log"

EVENT_NAME=push IN_ORCHESTRATED=false IN_CHANNEL='' IN_BASE_VERSION='' IN_TAG='' \
	REF_NAME=npm-v1.4.0-rc.1 RUN_NUMBER=50 GITHUB_OUTPUT="$test_root/npm-rc.out" \
	bash "$repo_root/scripts/resolve-npm-release.sh"
grep -Eq '^arg=npm-v1\.4\.0-rc\.1$' "$test_root/npm-rc.out"

EVENT_NAME=push IN_ORCHESTRATED=true IN_CHANNEL=stable IN_BASE_VERSION=1.5.0 \
	IN_TAG=npm-v1.5.0 REF_NAME=v1.5.0 RUN_NUMBER=51 GITHUB_OUTPUT="$test_root/npm-stable.out" \
	bash "$repo_root/scripts/resolve-npm-release.sh"
grep -Eq '^arg=v1\.5\.0$' "$test_root/npm-stable.out"

EVENT_NAME=workflow_call IN_ORCHESTRATED=true IN_CHANNEL=canary IN_BASE_VERSION=1.5.0 \
	IN_TAG='' REF_NAME=main-v2 RUN_NUMBER=999 IN_PREVIEW_NUMBER=42 \
	GITHUB_OUTPUT="$test_root/npm-canary.out" bash "$repo_root/scripts/resolve-npm-release.sh"
grep -Eq '^arg=v1\.5\.0-canary\.42$' "$test_root/npm-canary.out"

if EVENT_NAME=workflow_dispatch IN_ORCHESTRATED=false IN_CHANNEL=canary IN_BASE_VERSION=1.5.0 \
	IN_TAG='' REF_NAME=main-v2 RUN_NUMBER=52 GITHUB_OUTPUT="$test_root/npm-canary-direct.out" \
	bash "$repo_root/scripts/resolve-npm-release.sh" \
	>"$test_root/npm-canary-direct.log" 2>&1; then
	echo "standalone public npm Canary unexpectedly passed" >&2
	exit 1
fi
grep -Eq 'public npm Canary releases must be dispatched by release-preview.yml' \
	"$test_root/npm-canary-direct.log"

if EVENT_NAME=workflow_dispatch IN_ORCHESTRATED=false IN_CHANNEL=stable IN_BASE_VERSION=1.5.0 \
	IN_TAG=npm-v1.5.1 REF_NAME=main-v2 RUN_NUMBER=52 GITHUB_OUTPUT="$test_root/npm-mismatch.out" \
	bash "$repo_root/scripts/resolve-npm-release.sh" >"$test_root/npm-mismatch.log" 2>&1; then
	echo "mismatched npm stable dispatch unexpectedly passed" >&2
	exit 1
fi
grep -Eq 'does not match requested version' "$test_root/npm-mismatch.log"

node --test "$repo_root/npm/publish.test.mjs"

echo "release workflow contract tests: PASS"
