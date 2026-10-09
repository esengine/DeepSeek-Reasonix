#!/usr/bin/env bash
# Prints the `gh release create` flags that decide how GitHub ranks a Studio
# release, one per line. Usage: studio-release-flags.sh <tag> <candidate>,
# with the repository's studio-v* tags on stdin.
#
# A candidate (semver prerelease segment) is a prerelease and never latest. A
# stable tag is latest only when no higher stable studio tag exists, so
# recovering an older tag cannot take the badge back from a newer release.
set -euo pipefail

tag="${1:?tag}"
candidate="${2:?candidate}"

if [ "$candidate" = "true" ]; then
	printf '%s\n' --prerelease --latest=false
	exit 0
fi

stable='^studio-v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
highest="$(
	{ grep -E "$stable" || true; } |
		sed 's/^studio-v//' |
		sort -t. -k1,1n -k2,2n -k3,3n |
		tail -n 1
)"

if [ "studio-v$highest" = "$tag" ]; then
	printf '%s\n' --latest
else
	printf '%s\n' --latest=false
fi
