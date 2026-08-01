#!/usr/bin/env bash
set -euo pipefail

channel="${1:-}"
tag="${2:-}"
repository="${3:-}"
release_json="${4:-}"
checksums="${5:-}"

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
		echo "CLI publication channel must be stable, preview, or any: $channel" >&2
		exit 2
		;;
esac

if [[ ! "$tag" =~ $tag_pattern ]]; then
	echo "invalid $channel CLI publication tag: $tag" >&2
	exit 1
fi
if [[ ! "$repository" =~ ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$ ]]; then
	echo "invalid GitHub repository: $repository" >&2
	exit 1
fi

if [ "$release_json" = "-" ]; then
	echo publish
	exit 0
fi
if [ ! -f "$release_json" ]; then
	echo "CLI GitHub release record does not exist: $release_json" >&2
	exit 1
fi
if [ ! -f "$checksums" ]; then
	echo "CLI GitHub release checksum asset does not exist: $checksums" >&2
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
	--arg tag "$tag" \
	--arg repository "$repository" \
	--arg expected_prerelease "$expected_prerelease" \
	--argjson required "$required_assets" '
	(type == "object") and
	(.tag_name == $tag) and
	(.draft == false) and
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
		(.state == "uploaded") and
		(.size | type == "number" and . > 0 and . <= 1073741824 and floor == .) and
		(.browser_download_url ==
			("https://github.com/" + $repository + "/releases/download/" + $tag + "/" + .name)) and
		(.digest | type == "string" and test("^sha256:[0-9a-f]{64}$"))
	))
' "$release_json" >/dev/null || {
	echo "existing CLI GitHub release is incomplete or does not match approved release $tag" >&2
	exit 1
}

expected_checksums="$(mktemp)"
actual_checksums="$(mktemp)"
trap 'rm -f "$expected_checksums" "$actual_checksums"' EXIT

jq -r '
	.assets[] |
	select(.name != "SHA256SUMS") |
	((.digest | sub("^sha256:"; "")) + "  " + .name)
' "$release_json" | LC_ALL=C sort >"$expected_checksums"

if ! awk '
	BEGIN { valid = 1 }
	$0 !~ /^[0-9a-f]{64}  reasonix-(darwin|linux|windows)-(amd64|arm64)\.(tar\.gz|zip)$/ {
		valid = 0
	}
	{ print }
	END { if (NR != 6 || !valid) exit 1 }
' "$checksums" | LC_ALL=C sort >"$actual_checksums"; then
	echo "existing CLI SHA256SUMS is malformed or incomplete" >&2
	exit 1
fi

if ! cmp -s "$expected_checksums" "$actual_checksums"; then
	echo "existing CLI SHA256SUMS does not match GitHub asset digests" >&2
	exit 1
fi

checksum_asset_digest="$(
	jq -er '.assets[] | select(.name == "SHA256SUMS") | .digest | sub("^sha256:"; "")' \
		"$release_json"
)"
downloaded_checksum_digest="$(shasum -a 256 "$checksums" | awk '{print $1}')"
if [ "$downloaded_checksum_digest" != "$checksum_asset_digest" ]; then
	echo "downloaded CLI SHA256SUMS digest does not match GitHub release metadata" >&2
	exit 1
fi

echo reuse
