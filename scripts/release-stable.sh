#!/usr/bin/env bash
# Validate the exact remote main-v2 candidate, then push the three immutable
# Stable tags atomically. The v* tag activates the protected Stable relay.
set -euo pipefail

if [ "$#" -ne 1 ] || [[ ! "$1" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
	echo "usage: scripts/release-stable.sh MAJOR.MINOR.PATCH" >&2
	exit 2
fi

version="$1"
remote="${RELEASE_REMOTE:-origin}"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

for command in git gh jq node; do
	command -v "$command" >/dev/null || {
		echo "required command is unavailable: $command" >&2
		exit 2
	}
done

candidate="$(git ls-remote --heads "$remote" refs/heads/main-v2 | awk 'NR == 1 { print $1 }')"
if [[ ! "$candidate" =~ ^[0-9a-f]{40}$ ]]; then
	echo "cannot resolve $remote/main-v2" >&2
	exit 1
fi
git fetch --quiet --no-tags "$remote" refs/heads/main-v2
bash "$script_dir/validate-stable-candidate.sh" "$version" "$candidate"

tags=("v$version" "npm-v$version" "desktop-v$version")
for tag in "${tags[@]}"; do
	if git ls-remote --exit-code --tags --refs "$remote" "refs/tags/$tag" >/dev/null 2>&1; then
		echo "release tag already exists and will not be moved: $tag" >&2
		exit 1
	fi
done

bash "$script_dir/verify-release-push-ci.sh" "$candidate"

# Include a no-op main-v2 update in the same atomic transaction. If main-v2
# advanced while CI was running, this refspec becomes a non-fast-forward update
# and the server rejects every tag instead of burning an unreleasable version.
git push --atomic "$remote" \
	"$candidate:refs/heads/main-v2" \
	"$candidate:refs/tags/${tags[0]}" \
	"$candidate:refs/tags/${tags[1]}" \
	"$candidate:refs/tags/${tags[2]}"

for tag in "${tags[@]}"; do
	remote_sha="$(git ls-remote --tags --refs "$remote" "refs/tags/$tag" | awk 'NR == 1 { print $1 }')"
	if [ "$remote_sha" != "$candidate" ]; then
		echo "$tag resolved to ${remote_sha:-missing}; expected $candidate" >&2
		exit 1
	fi
done

echo "Stable tags pushed atomically at $candidate: ${tags[*]}"
echo "Release stable will request one release-environment approval."
