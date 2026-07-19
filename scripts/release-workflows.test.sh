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
for workflow in release.yml release-npm.yml release-desktop.yml; do
	grep -Eq 'github\.workflow_ref' "$repo_root/.github/workflows/$workflow"
	grep -Eq 'github\.ref_protected' "$repo_root/.github/workflows/$workflow"
	grep -Eq 'inputs\.approved_sha' "$repo_root/.github/workflows/$workflow"
	grep -Eq 'verify-release-tag\.sh' "$repo_root/.github/workflows/$workflow"
	grep -Eq 'release-stable\.yml' "$repo_root/.github/workflows/$workflow"
	grep -Eq "needs\.cache-guard\.result == 'success'" "$repo_root/.github/workflows/$workflow"
done
grep -Eq "needs\.build\.result == 'success'" "$repo_root/.github/workflows/release-desktop.yml"
grep -Eq "needs\.publish\.result == 'success'" "$repo_root/.github/workflows/release-desktop.yml"
grep -Eq '^  postflight:$' "$repo_root/.github/workflows/release-stable.yml"
grep -Eq 'verify-stable-release-artifacts\.sh' "$repo_root/.github/workflows/release-stable.yml"

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
	git push -q origin v1.2.3 npm-v1.2.3 desktop-v1.2.3
	GITHUB_OUTPUT="$test_root/stable.out" RELEASE_TAG=v1.2.3 "$repo_root/scripts/resolve-stable-release.sh"
	grep -Eq '^version=1\.2\.3$' "$test_root/stable.out"
	grep -Eq '^desktop_tag=desktop-v1\.2\.3$' "$test_root/stable.out"
	approved_sha="$(git rev-parse HEAD)"
	ACTUAL_CALLER_WORKFLOW_REF='example/reasonix/.github/workflows/release-stable.yml@refs/tags/v1.2.3' \
		EXPECTED_CALLER_WORKFLOW_REF='example/reasonix/.github/workflows/release-stable.yml@refs/tags/v1.2.3' \
		CALLER_EVENT_NAME=push CALLER_REF=refs/tags/v1.2.3 CALLER_REF_PROTECTED=true \
		CALLER_WORKFLOW_SHA="$approved_sha" CALLER_SHA="$approved_sha" \
		APPROVED_CLI_TAG=v1.2.3 APPROVED_SHA="$approved_sha" \
		"$repo_root/scripts/verify-release-authorization.sh"
	RELEASE_TAG=desktop-v1.2.3 APPROVED_SHA="$approved_sha" \
		"$repo_root/scripts/verify-release-tag.sh"

	git commit --allow-empty -q -m "release workflow fix"
	git push -q origin main-v2
	recovery_workflow_sha="$(git rev-parse HEAD)"
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

EVENT_NAME=workflow_dispatch IN_CHANNEL=canary IN_BASE_VERSION=1.3.0 IN_TAG='' REF_NAME=main-v2 RUN_NUMBER=42 \
	GITHUB_OUTPUT="$test_root/desktop-canary.out" bash "$repo_root/scripts/resolve-desktop-release.sh"
grep -Eq '^version=v1\.3\.0-canary\.42$' "$test_root/desktop-canary.out"

EVENT_NAME=push IN_CHANNEL='' IN_TAG='' REF_NAME=desktop-v1.4.0-rc.1 RUN_NUMBER=50 \
	GITHUB_OUTPUT="$test_root/desktop-rc.out" bash "$repo_root/scripts/resolve-desktop-release.sh"
grep -Eq '^prerelease=true$' "$test_root/desktop-rc.out"

if EVENT_NAME=workflow_dispatch IN_CHANNEL=stable IN_TAG='' REF_NAME=main-v2 RUN_NUMBER=50 \
	GITHUB_OUTPUT="$test_root/desktop-missing-tag.out" bash "$repo_root/scripts/resolve-desktop-release.sh" \
	>"$test_root/desktop-missing-tag.log" 2>&1; then
	echo "tag-less desktop stable dispatch unexpectedly passed" >&2
	exit 1
fi
grep -Eq 'stable dispatch requires tag' "$test_root/desktop-missing-tag.log"

EVENT_NAME=push IN_ORCHESTRATED=false IN_CHANNEL='' IN_BASE_VERSION='' IN_TAG='' \
	REF_NAME=npm-v1.4.0-rc.1 RUN_NUMBER=50 GITHUB_OUTPUT="$test_root/npm-rc.out" \
	bash "$repo_root/scripts/resolve-npm-release.sh"
grep -Eq '^arg=npm-v1\.4\.0-rc\.1$' "$test_root/npm-rc.out"

EVENT_NAME=push IN_ORCHESTRATED=true IN_CHANNEL=stable IN_BASE_VERSION=1.5.0 \
	IN_TAG=npm-v1.5.0 REF_NAME=v1.5.0 RUN_NUMBER=51 GITHUB_OUTPUT="$test_root/npm-stable.out" \
	bash "$repo_root/scripts/resolve-npm-release.sh"
grep -Eq '^arg=v1\.5\.0$' "$test_root/npm-stable.out"

if EVENT_NAME=workflow_dispatch IN_ORCHESTRATED=false IN_CHANNEL=stable IN_BASE_VERSION=1.5.0 \
	IN_TAG=npm-v1.5.1 REF_NAME=main-v2 RUN_NUMBER=52 GITHUB_OUTPUT="$test_root/npm-mismatch.out" \
	bash "$repo_root/scripts/resolve-npm-release.sh" >"$test_root/npm-mismatch.log" 2>&1; then
	echo "mismatched npm stable dispatch unexpectedly passed" >&2
	exit 1
fi
grep -Eq 'does not match requested version' "$test_root/npm-mismatch.log"

echo "release workflow contract tests: PASS"
