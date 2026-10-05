#!/usr/bin/env bash
# Validate one canonical Preview release tag and emit the shared release event
# identity used by CLI, Desktop, npm, and the exact changelog entry.
set -euo pipefail

release_tag="${RELEASE_TAG:?RELEASE_TAG is required}"
release_remote="${RELEASE_REMOTE:-origin}"
allow_recovery="${ALLOW_PREVIEW_RECOVERY:-false}"

case "$allow_recovery" in
true | false) ;;
*)
	echo "::error::ALLOW_PREVIEW_RECOVERY must be true or false, got: $allow_recovery" >&2
	exit 2
	;;
esac

if [[ ! "$release_tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-preview\.([1-9][0-9]*)$ ]]; then
	echo "::error::Preview release tag must be vMAJOR.MINOR.PATCH-preview.N, got: $release_tag" >&2
	exit 1
fi

base_version="${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.${BASH_REMATCH[3]}"
preview_number="${BASH_REMATCH[4]}"
checkout_sha="$(git rev-parse HEAD^{commit})"
main_sha="$(git ls-remote "$release_remote" refs/heads/main-v2 | awk 'NR == 1 { print $1 }')"
tag_sha="$(
	git ls-remote --tags "$release_remote" "refs/tags/$release_tag" "refs/tags/$release_tag^{}" |
		awk '/\^\{\}$/ { print $1; found = 1; exit } NR == 1 { first = $1 } END { if (!found) print first }'
)"

if [ -z "$main_sha" ] || [ "$checkout_sha" != "$main_sha" ]; then
	echo "::error::Preview control checkout must equal current $release_remote/main-v2 ($main_sha), got $checkout_sha" >&2
	exit 1
fi
if [ -z "$tag_sha" ]; then
	echo "::error::$release_tag is missing from $release_remote" >&2
	exit 1
fi
if [ "$tag_sha" != "$main_sha" ]; then
	if [ "$allow_recovery" != "true" ]; then
		echo "::error::$release_tag must point to current $release_remote/main-v2 ($main_sha), got $tag_sha" >&2
		exit 1
	fi
	if ! git cat-file -e "$tag_sha^{commit}" 2>/dev/null ||
		! git merge-base --is-ancestor "$tag_sha" "$main_sha"; then
		echo "::error::Preview recovery tag $release_tag must remain on $release_remote/main-v2 history" >&2
		exit 1
	fi
fi

{
	echo "version=${release_tag#v}"
	echo "base_version=$base_version"
	echo "preview_number=$preview_number"
	echo "cli_tag=$release_tag"
	echo "desktop_tag=desktop-v${base_version}-preview.${preview_number}"
	echo "npm_version=${base_version}-canary.${preview_number}"
	echo "sha=$tag_sha"
} >>"${GITHUB_OUTPUT:-/dev/stdout}"

echo "Preview release resolved: ${release_tag#v} at $tag_sha (recovery=$allow_recovery)"
