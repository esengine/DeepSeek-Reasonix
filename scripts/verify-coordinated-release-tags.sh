#!/usr/bin/env bash
# Verify that the three release artifact families for one product version are
# sourced from exactly the same commit. Remote Build IDs include the source
# revision, so publishing one family from a different commit would create an
# installation that can never connect successfully.
set -euo pipefail

input_tag="${1:?usage: verify-coordinated-release-tags.sh <vX|npm-vX|desktop-vX> <commit>}"
expected_commit="${2:?usage: verify-coordinated-release-tags.sh <vX|npm-vX|desktop-vX> <commit>}"
remote="${RELEASE_TAG_REMOTE:-origin}"

case "$input_tag" in
	npm-v*) version="v${input_tag#npm-v}" ;;
	desktop-v*) version="v${input_tag#desktop-v}" ;;
	v*) version="$input_tag" ;;
	*)
		echo "release tag must use vX, npm-vX, or desktop-vX: $input_tag" >&2
		exit 2
		;;
esac

if [[ ! "$expected_commit" =~ ^[0-9a-f]{40}$ ]]; then
	echo "release source commit must be a full lowercase Git commit: $expected_commit" >&2
	exit 2
fi

suffix="${version#v}"
if [ -z "$suffix" ]; then
	echo "release tag has an empty product version: $input_tag" >&2
	exit 2
fi

tags=("v${suffix}" "npm-v${suffix}" "desktop-v${suffix}")
remote_refs=()
for tag in "${tags[@]}"; do
	if ! git check-ref-format "refs/tags/$tag" >/dev/null; then
		echo "invalid coordinated release tag: $tag" >&2
		exit 2
	fi
	remote_refs+=("refs/tags/$tag" "refs/tags/$tag^{}")
done

if ! remote_output="$(git ls-remote "$remote" "${remote_refs[@]}")"; then
	echo "cannot query coordinated release tags from $remote" >&2
	exit 1
fi

resolve_tag_commit() {
	local tag="$1" direct="" peeled="" sha ref
	while read -r sha ref; do
		[ -n "${sha:-}" ] || continue
		case "$ref" in
			"refs/tags/$tag") direct="$sha" ;;
			"refs/tags/$tag^{}") peeled="$sha" ;;
		esac
	done <<<"$remote_output"
	if [ -z "$direct" ]; then
		echo "missing coordinated release tag $tag on $remote" >&2
		return 1
	fi
	if [ -n "$peeled" ]; then
		printf '%s' "$peeled"
	else
		printf '%s' "$direct"
	fi
}

for tag in "${tags[@]}"; do
	commit="$(resolve_tag_commit "$tag")" || exit 1
	if [[ ! "$commit" =~ ^[0-9a-f]{40}$ ]]; then
		echo "release tag $tag did not resolve to a full commit: $commit" >&2
		exit 1
	fi
	if [ "$commit" != "$expected_commit" ]; then
		echo "release tag $tag resolves to $commit, expected $expected_commit" >&2
		exit 1
	fi
	echo "$tag -> $commit"
done

echo "coordinated release tags for $version all resolve to $expected_commit"
