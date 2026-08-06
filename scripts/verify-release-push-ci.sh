#!/usr/bin/env bash
# Wait for successful push CI on one immutable main-v2 candidate.
set -euo pipefail

if [ "$#" -ne 1 ] || [[ ! "$1" =~ ^[0-9a-f]{40}$ ]]; then
	echo "usage: verify-release-push-ci.sh FULL_COMMIT_SHA" >&2
	exit 2
fi

candidate="$1"
repository="${RELEASE_REPOSITORY:-esengine/DeepSeek-Reasonix}"
wait_seconds="${RELEASE_CI_WAIT_SECONDS:-1800}"
poll_seconds="${RELEASE_CI_POLL_SECONDS:-10}"

if [[ ! "$wait_seconds" =~ ^[0-9]+$ ]] || [[ ! "$poll_seconds" =~ ^[1-9][0-9]*$ ]]; then
	echo "RELEASE_CI_WAIT_SECONDS must be non-negative and RELEASE_CI_POLL_SECONDS must be positive" >&2
	exit 2
fi
for command in gh jq; do
	command -v "$command" >/dev/null || {
		echo "required command is unavailable: $command" >&2
		exit 2
	}
done

deadline=$((SECONDS + wait_seconds))
while :; do
	runs="$(gh run list --repo "$repository" --workflow ci.yml --commit "$candidate" \
		--event push --limit 20 --json headSha,status,conclusion 2>/dev/null || true)"
	run_status="$(jq -r --arg sha "$candidate" \
		'[.[] | select(.headSha == $sha)][0].status // ""' <<<"$runs" 2>/dev/null || true)"
	conclusion="$(jq -r --arg sha "$candidate" \
		'[.[] | select(.headSha == $sha and .status == "completed")][0].conclusion // ""' <<<"$runs" 2>/dev/null || true)"
	case "$conclusion" in
	success)
		echo "successful main-v2 push CI verified: $candidate"
		exit 0
		;;
	failure | cancelled | timed_out | action_required | startup_failure)
		echo "CI for $candidate concluded $conclusion" >&2
		exit 1
		;;
	esac
	if [ "$SECONDS" -ge "$deadline" ]; then
		echo "timed out waiting for successful main-v2 CI on $candidate (last status: ${run_status:-missing})" >&2
		exit 1
	fi
	sleep "$poll_seconds"
done
