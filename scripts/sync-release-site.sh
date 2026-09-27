#!/usr/bin/env bash
# One post-publication owner for fresh observations, Pages, and durable evidence.
set -euo pipefail
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repository="${RELEASE_REPOSITORY:?}"
version="${RELEASE_VERSION:?}"
operation="${RELEASE_OPERATION:?}"
expected_sha="${RELEASE_EXPECTED_SHA:?}"
output="${RELEASE_LEDGER_OUTPUT:?}"
site_recovery="${RELEASE_SITE_RECOVERY_ONLY:-false}"
[[ "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || exit 2
[[ "$expected_sha" =~ ^[0-9a-f]{40}$ ]] || exit 2
[[ "$operation" =~ ^(publish|recover)$ ]] || exit 2
[[ "$site_recovery" =~ ^(true|false)$ ]] || exit 2
test "$repository" = esengine/DeepSeek-Reasonix || exit 2
if [ "${GITHUB_ACTIONS:-}" = true ]; then
	test "$GITHUB_REPOSITORY" = "$repository"
	test "$GITHUB_REF" = refs/heads/main-v2
	test "$GITHUB_REF_PROTECTED" = true
fi
work="$(mktemp -d)"
mkdir -p "$(dirname "$output")"
state=verification-failed
stage=immutable
pages_run=""
jq -n --arg version "$version" --arg sha "$expected_sha" --arg operation "$operation" \
	'{schema:1,version:$version,sourceSHA:$sha,operation:$operation,surfaces:{}}' > "$work/ledger.json"
finish() {
	local status=$?
	trap - EXIT
	jq --arg state "$state" --arg stage "$stage" --arg pages "$pages_run" \
		--arg run "${GITHUB_RUN_ID:-local}" --arg attempt "${GITHUB_RUN_ATTEMPT:-1}" \
		--arg control "${GITHUB_SHA:-}" --argjson exitCode "$status" \
		'. + {observedAt:(now|todateiso8601),completionState:$state,verificationContext:{stage:$stage,exitCode:$exitCode,runId:$run,runAttempt:$attempt,controlSHA:$control,pagesRunId:$pages}}' \
		"$work/ledger.json" > "$output" || status=1
	rm -rf -- "$work"
	exit "$status"
}
trap finish EXIT
export CLI_TAG="v$version" DESKTOP_TAG="desktop-v$version"
RELEASE_LEDGER_OUTPUT="$work/core.json" VERIFY_HOMEPAGE=false VERIFY_PUBLIC_SITE_ONLY=false \
	bash "$script_dir/verify-stable-release-artifacts.sh"
cp "$work/core.json" "$work/ledger.json"
state=public-sync-pending
stage=release-event
node "$script_dir/release-event.mjs" generate --version "$version" --sha "$expected_sha" \
	--published-at "$(gh api "repos/$repository/releases/tags/v$version" --jq .published_at)" \
	--output "$work/release-event.json"
has_event="$(gh release view "v$version" --repo "$repository" --json assets --jq '[.assets[] | select(.name == "release-event.json")] | length')"
if [ "$has_event" = 1 ]; then
	gh release download "v$version" --repo "$repository" --pattern release-event.json --output "$work/existing-event.json"
	cmp -s "$work/release-event.json" "$work/existing-event.json" || { echo 'Published release event conflicts with the verified identity' >&2; exit 1; }
elif [ "$has_event" = 0 ] && [ "$site_recovery" = false ]; then
	gh release upload "v$version" --repo "$repository" "$work/release-event.json"
else
	echo 'Site recovery requires one existing immutable release-event.json; use full recovery to repair publication' >&2
	exit 1
fi
stage=ownership
update="$(bash "$script_dir/observe-release-site.sh" "$version" "$operation")"
if [ "$update" = false ]; then
	state=immutable-complete-newer-pointer-preserved
	stage=complete
	exit 0
fi
test "$update" = true
stage=pages-dispatch
request="release-${GITHUB_RUN_ID:?}-${GITHUB_RUN_ATTEMPT:?}"
title="Deploy site v$version [$request]"
payload="$(jq -cn --arg version "$version" --arg request "$request" '{ref:"website",inputs:{release_version:$version,release_request:$request}}')"
# Dispatch once. On an uncertain response, recovery observes public state again;
# never blindly repeat this write within the same run.
gh api -X POST "repos/$repository/actions/workflows/pages.yml/dispatches" --input - <<< "$payload"
for _ in $(seq 1 30); do
	runs="$(gh run list --repo "$repository" --workflow pages.yml --event workflow_dispatch --branch website --limit 100 --json databaseId,displayTitle)"
	pages_run="$(jq -er --arg title "$title" '[.[] | select(.displayTitle == $title)] | if length == 0 then "" elif length == 1 then .[0].databaseId else error("ambiguous owned Pages deployment") end' <<< "$runs")"
	[ -z "$pages_run" ] || break
	sleep 2
done
test -n "$pages_run" || { echo 'public-sync-pending: owned Pages deployment was not found' >&2; exit 1; }
stage=pages
gh run watch "$pages_run" --repo "$repository" --exit-status
test "$(gh run view "$pages_run" --repo "$repository" --json conclusion --jq .conclusion)" = success
stage=public-site
RELEASE_LEDGER_OUTPUT="$work/site.json" VERIFY_HOMEPAGE=true VERIFY_PUBLIC_SITE_ONLY=true \
	bash "$script_dir/verify-stable-release-artifacts.sh"
node "$script_dir/release-publication-ledger.mjs" merge "$work/core.json" "$work/site.json" "$work/ledger.json"
state=complete
stage=complete
