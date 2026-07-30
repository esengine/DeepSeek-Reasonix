#!/usr/bin/env bash
# Resolve and authorize the immutable source commit for a Desktop publication.
# The workflow control plane must run from protected main-v2. Stable/RC builds
# use an existing tag on main-v2 history; Preview builds use the exact main-v2
# commit selected when the protected workflow was dispatched.
set -euo pipefail

channel="${RELEASE_CHANNEL:?RELEASE_CHANNEL is required}"
tag="${RELEASE_TAG:?RELEASE_TAG is required}"
orchestrated="${IN_ORCHESTRATED:-false}"
approved_sha="${APPROVED_SHA:-}"
caller_event="${CALLER_EVENT_NAME:-}"
caller_ref="${CALLER_REF:-}"
caller_ref_protected="${CALLER_REF_PROTECTED:-}"
caller_sha="${CALLER_SHA:-}"
caller_workflow_sha="${CALLER_WORKFLOW_SHA:-}"
release_remote="${RELEASE_REMOTE:-origin}"
require_current_main="${REQUIRE_CURRENT_MAIN:-true}"
verify_checkout="${VERIFY_RELEASE_CHECKOUT:-false}"

case "$orchestrated" in
true | false) ;;
*)
	echo "::error::IN_ORCHESTRATED must be true or false, got: $orchestrated" >&2
	exit 2
	;;
esac
case "$require_current_main" in
true | false) ;;
*)
	echo "::error::REQUIRE_CURRENT_MAIN must be true or false, got: $require_current_main" >&2
	exit 2
	;;
esac
case "$verify_checkout" in
true | false) ;;
*)
	echo "::error::VERIFY_RELEASE_CHECKOUT must be true or false, got: $verify_checkout" >&2
	exit 2
	;;
esac

stable_tag_pattern='^desktop-v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+)(\.[0-9A-Za-z-]+)*)?)?$'
preview_tag_pattern='^desktop-v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-preview\.(0|[1-9][0-9]*)$'

case "$channel" in
stable)
	if [[ ! "$tag" =~ $stable_tag_pattern ]] || [[ "$tag" =~ $preview_tag_pattern ]]; then
		echo "::error::Stable Desktop candidate requires desktop-vMAJOR.MINOR.PATCH[-PRERELEASE], excluding -preview.N: $tag" >&2
		exit 1
	fi
	;;
preview)
	if [[ ! "$tag" =~ $preview_tag_pattern ]]; then
		echo "::error::Preview Desktop candidate requires desktop-vMAJOR.MINOR.PATCH-preview.N: $tag" >&2
		exit 1
	fi
	if [ "$orchestrated" = "true" ]; then
		echo "::error::the stable orchestrator cannot authorize a Desktop Preview candidate" >&2
		exit 1
	fi
	;;
*)
	echo "::error::Desktop release channel must be stable or preview, got: $channel" >&2
	exit 2
	;;
esac

git fetch "$release_remote" main-v2 --tags
main_sha="$(git rev-parse "$release_remote/main-v2^{commit}")"

if [ "$orchestrated" = "true" ]; then
	candidate="$approved_sha"
else
	if [ "$caller_event" != "workflow_dispatch" ] ||
		[ "$caller_ref" != "refs/heads/main-v2" ] ||
		[ "$caller_ref_protected" != "true" ]; then
		echo "::error::standalone Desktop releases must run from protected main-v2" >&2
		exit 1
	fi
	if [ "$caller_sha" != "$caller_workflow_sha" ]; then
		echo "::error::standalone Desktop workflow SHA is $caller_workflow_sha, expected caller SHA $caller_sha" >&2
		exit 1
	fi
	if [ "$channel" = "preview" ]; then
		candidate="$caller_sha"
	else
		candidate="$(git rev-parse "$tag^{commit}")"
	fi
fi

if [[ ! "$candidate" =~ ^[0-9a-f]{40}$ ]]; then
	echo "::error::Desktop candidate SHA must be a full commit SHA, got: $candidate" >&2
	exit 1
fi
if ! git cat-file -e "$candidate^{commit}" 2>/dev/null; then
	echo "::error::Desktop candidate commit is unavailable: $candidate" >&2
	exit 1
fi
if ! git merge-base --is-ancestor "$candidate" "$main_sha"; then
	echo "::error::Desktop candidate $candidate is not on main-v2 history at $main_sha" >&2
	exit 1
fi

if [ "$channel" = "stable" ]; then
	tag_sha="$(git rev-parse "$tag^{commit}")"
	if [ "$tag_sha" != "$candidate" ]; then
		echo "::error::$tag points to $tag_sha, expected Desktop candidate $candidate" >&2
		exit 1
	fi
elif git show-ref --verify --quiet "refs/tags/$tag"; then
	echo "::error::Desktop Preview uses an immutable asset directory, not a Git tag: $tag" >&2
	exit 1
elif [ "$require_current_main" = "true" ] && [ "$candidate" != "$main_sha" ]; then
	echo "::error::Desktop Preview must use current main-v2 $main_sha, got: $candidate" >&2
	exit 1
fi

if [ "$verify_checkout" = "true" ]; then
	head_sha="$(git rev-parse HEAD^{commit})"
	if [ "$head_sha" != "$candidate" ]; then
		echo "::error::Desktop release checkout is $head_sha, expected candidate $candidate" >&2
		exit 1
	fi
fi

echo "sha=$candidate" >>"${GITHUB_OUTPUT:-/dev/stdout}"
echo "Desktop candidate verified: channel=$channel tag=$tag sha=$candidate"
