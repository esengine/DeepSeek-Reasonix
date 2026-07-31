#!/usr/bin/env bash
set -euo pipefail

channel="${1:-}"
tag="${2:-}"
repository="${3:-}"
manifest="${4:-}"

stable_tag_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
preview_tag_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-preview\.(0|[1-9][0-9]*)$'
release_tag_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+)(\.[0-9A-Za-z-]+)*)?)?$'

case "$channel" in
	stable)
		tag_pattern="$stable_tag_pattern"
		expected_prerelease=false
		;;
	preview)
		tag_pattern="$preview_tag_pattern"
		expected_prerelease=true
		;;
	any)
		tag_pattern="$release_tag_pattern"
		expected_prerelease=any
		;;
	*)
		echo "CLI manifest channel must be stable, preview, or any: $channel" >&2
		exit 2
		;;
esac

if [[ ! "$tag" =~ $tag_pattern ]]; then
	echo "invalid $channel CLI manifest tag: $tag" >&2
	exit 1
fi
if [[ ! "$repository" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]; then
	echo "invalid GitHub repository: $repository" >&2
	exit 1
fi
if [ ! -f "$manifest" ]; then
	echo "CLI release manifest does not exist: $manifest" >&2
	exit 1
fi

required_assets='[
  "reasonix-darwin-amd64.tar.gz",
  "reasonix-darwin-arm64.tar.gz",
  "reasonix-linux-amd64.tar.gz",
  "reasonix-linux-arm64.tar.gz",
  "reasonix-windows-amd64.zip",
  "reasonix-windows-arm64.zip",
  "SHA256SUMS"
]'

jq -e \
	--arg channel "$channel" \
	--arg tag "$tag" \
	--arg repository "$repository" \
	--arg expected_prerelease "$expected_prerelease" \
	--argjson required "$required_assets" '
	(type == "object") and
	(.tag_name == $tag) and
	(if $expected_prerelease == "any"
		then (.prerelease | type == "boolean")
		else (.prerelease == ($expected_prerelease == "true"))
	end) and
	(.html_url == ("https://github.com/" + $repository + "/releases/tag/" + $tag)) and
	(.assets | type == "array") and
	(.assets | length == ($required | length)) and
	((.assets | map(.name) | sort) == ($required | sort)) and
	(.assets | all(
		(type == "object") and
		(.name | type == "string") and
		(.browser_download_url ==
			("https://github.com/" + $repository + "/releases/download/" + $tag + "/" + .name)) and
			(.size | type == "number" and . > 0 and . <= 1073741824 and floor == .)
		))
	' "$manifest" >/dev/null
