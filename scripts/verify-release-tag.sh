#!/usr/bin/env bash
# Verify that a release job is building the approved commit and that the remote
# tag still resolves to that commit. This closes the gap between the stable
# preflight/approval and the later publishing jobs.
set -euo pipefail

release_tag="${RELEASE_TAG:?RELEASE_TAG is required}"
approved_sha="${APPROVED_SHA:?APPROVED_SHA is required}"
release_remote="${RELEASE_REMOTE:-origin}"
verify_checkout="${VERIFY_RELEASE_CHECKOUT:-true}"

case "$verify_checkout" in
true | false) ;;
*)
	echo "::error::VERIFY_RELEASE_CHECKOUT must be true or false, got: $verify_checkout" >&2
	exit 1
	;;
esac

if [[ ! "$approved_sha" =~ ^[0-9a-f]{40}$ ]]; then
	echo "::error::approved release SHA must be a full commit SHA, got: $approved_sha" >&2
	exit 1
fi
if ! git check-ref-format "refs/tags/$release_tag" >/dev/null; then
	echo "::error::invalid release tag: $release_tag" >&2
	exit 1
fi

if [ "$verify_checkout" = "true" ]; then
	head_sha="$(git rev-parse HEAD^{commit})"
	if [ "$head_sha" != "$approved_sha" ]; then
		echo "::error::release checkout is $head_sha, expected approved SHA $approved_sha" >&2
		exit 1
	fi
fi

# Prefer the peeled commit for annotated tags; lightweight tags only return the
# first line. Both forms are valid release refs.
tag_sha="$(
	git ls-remote --tags "$release_remote" "refs/tags/$release_tag" "refs/tags/$release_tag^{}" |
		awk '/\^\{\}$/ { print $1; found = 1; exit } NR == 1 { first = $1 } END { if (!found) print first }'
)"
if [ -z "$tag_sha" ]; then
	echo "::error::approved release tag is missing: $release_tag" >&2
	exit 1
fi
if [ "$tag_sha" != "$approved_sha" ]; then
	echo "::error::$release_tag moved to $tag_sha after approval; expected $approved_sha" >&2
	exit 1
fi

echo "release tag verified: tag=$release_tag sha=$approved_sha"
