#!/usr/bin/env bash
# Build the real CLI entrypoint, then prove its embedded docs corpus and build
# identity match the exact release checkout before any publisher starts.
set -euo pipefail

if [ "$#" -ne 2 ]; then
	echo "usage: verify-embedded-docs.sh VERSION_OR_TAG FULL_COMMIT_SHA" >&2
	exit 2
fi

version="$1"
revision="$2"
case "$version" in
	npm-v*) version="v${version#npm-v}" ;;
	desktop-v*) version="v${version#desktop-v}" ;;
	v*) ;;
	*) version="v$version" ;;
esac
version_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+)(\.[0-9A-Za-z-]+)*)?)?$'
if [[ ! "$version" =~ $version_pattern ]]; then
	echo "invalid docs build version: $version" >&2
	exit 2
fi
if [[ ! "$revision" =~ ^[0-9a-f]{40}$ ]]; then
	echo "invalid docs source revision: $revision" >&2
	exit 2
fi

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/reasonix-docs-verify.XXXXXX")"
cleanup() {
	case "$tmp_dir" in
	*/reasonix-docs-verify.*) rm -rf -- "$tmp_dir" ;;
	*) echo "refusing to clean unexpected docs verification directory: $tmp_dir" >&2 ;;
	esac
}
trap cleanup EXIT

binary="$tmp_dir/reasonix"
ldflags="-s -w -X main.version=$version -X reasonix/internal/productdocs.linkedVersion=$version -X reasonix/internal/productdocs.linkedRevision=$revision"
(
	cd "$repo_root"
	CGO_ENABLED=0 go build -trimpath -ldflags="$ldflags" -o "$binary" ./cmd/reasonix
)

mkdir -p "$tmp_dir/reasonix-home"
manifest="$(
	REASONIX_HOME="$tmp_dir/reasonix-home" "$binary" docs-manifest \
		--verify-source "$repo_root" \
		--expect-version "$version" \
		--expect-revision "$revision"
)"
printf 'Embedded docs verified: %s\n' "$manifest"
