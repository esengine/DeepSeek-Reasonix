#!/usr/bin/env bash
set -euo pipefail

manifest="${1:-}"
asset_dir="${2:-}"

if [ ! -f "$manifest" ] || [ ! -d "$asset_dir" ]; then
	echo "usage: $0 MANIFEST ASSET_DIRECTORY" >&2
	exit 2
fi

entries="$(mktemp)"
trap 'rm -f "$entries"' EXIT
jq -er '
	[.platforms, .native_packages, (.downloads // {})]
	| map(to_entries)
	| add[]
	| [.value.url, .value.sha256, (.value.size | tostring)]
	| @tsv
' "$manifest" >"$entries"

while IFS=$'\t' read -r url expected_sha expected_size; do
	name="${url##*/}"
	if [[ ! "$name" =~ ^[A-Za-z0-9][A-Za-z0-9._-]*$ ]]; then
		echo "Desktop release manifest has an unsafe asset URL: $url" >&2
		exit 1
	fi
	asset="$asset_dir/$name"
	if [ ! -f "$asset" ]; then
		echo "Desktop release manifest asset is missing: $name" >&2
		exit 1
	fi
	actual_size="$(wc -c <"$asset" | tr -d '[:space:]')"
	if [ "$actual_size" != "$expected_size" ]; then
		echo "Desktop release manifest size does not match $name" >&2
		exit 1
	fi
	actual_sha="$(shasum -a 256 "$asset" | awk '{print $1}')"
	if [ "$actual_sha" != "$expected_sha" ]; then
		echo "Desktop release manifest digest does not match $name" >&2
		exit 1
	fi
done <"$entries"
