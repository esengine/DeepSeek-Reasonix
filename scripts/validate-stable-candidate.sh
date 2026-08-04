#!/usr/bin/env bash
# Bind a normal official release to the main-v2 commit that introduced or
# updated its reviewed release-notes record.
set -euo pipefail

if [ "$#" -ne 2 ] || [[ ! "$1" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || \
	[[ ! "$2" =~ ^[0-9a-f]{40}$ ]]; then
	echo "usage: validate-stable-candidate.sh MAJOR.MINOR.PATCH FULL_COMMIT_SHA" >&2
	exit 2
fi

version="$1"
candidate="$2"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

git cat-file -e "$candidate^{commit}" 2>/dev/null || {
	echo "release candidate commit is unavailable: $candidate" >&2
	exit 1
}
parent="$(git rev-parse "$candidate^1" 2>/dev/null)" || {
	echo "release candidate must have a first parent: $candidate" >&2
	exit 1
}

work_dir="$(mktemp -d "${TMPDIR:-/tmp}/reasonix-stable-candidate.XXXXXX")"
cleanup() {
	case "$work_dir" in
	*/reasonix-stable-candidate.*) rm -rf -- "$work_dir" ;;
	*) echo "refusing to clean unexpected candidate directory: $work_dir" >&2 ;;
	esac
}
trap cleanup EXIT

git show "$candidate:release-notes/releases.json" >"$work_dir/current.json" || {
	echo "release candidate does not contain release-notes/releases.json: $candidate" >&2
	exit 1
}
git show "$parent:release-notes/releases.json" >"$work_dir/previous.json" || {
	echo "release candidate parent does not contain release-notes/releases.json: $parent" >&2
	exit 1
}

node "$script_dir/validate-stable-release-record.mjs" \
	"$work_dir/current.json" "$version" "$work_dir/previous.json"
echo "Stable candidate is bound to its reviewed Notes change: v$version at $candidate"
