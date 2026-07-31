#!/usr/bin/env bash
set -euo pipefail

channel="${1:-}"
version="${2:-}"
asset_base="${3:-}"
manifest="${4:-}"
notes_version="${5:-$version}"

stable_version_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
preview_version_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-preview\.(0|[1-9][0-9]*)$'
release_version_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+)(\.[0-9A-Za-z-]+)*)?)?$'

case "$channel" in
	stable)
		version_pattern="$stable_version_pattern"
		legacy=false
		;;
	preview)
		version_pattern="$preview_version_pattern"
		legacy=false
		;;
	any)
		version_pattern="$release_version_pattern"
		legacy=false
		;;
	legacy-stable)
		version_pattern="$stable_version_pattern"
		legacy=true
		;;
	legacy-preview)
		version_pattern="$preview_version_pattern"
		legacy=true
		;;
	legacy-any)
		version_pattern="$release_version_pattern"
		legacy=true
		;;
	*)
		echo "Desktop manifest channel must be stable, preview, any, legacy-stable, legacy-preview, or legacy-any: $channel" >&2
		exit 2
		;;
esac

if [[ ! "$version" =~ $version_pattern ]]; then
	echo "invalid $channel Desktop manifest version: $version" >&2
	exit 1
fi
if [[ ! "$notes_version" =~ $release_version_pattern ]]; then
	echo "invalid Desktop release-notes version: $notes_version" >&2
	exit 1
fi

expected_tag="desktop-${version}"
github_base="https://github.com/esengine/DeepSeek-Reasonix/releases/download/${expected_tag}/"
r2_base="https://dl.reasonix.io/${expected_tag}/"
legacy_preview_base="https://dl.reasonix.io/desktop-preview/"
if [ "$channel" = "legacy-preview" ]; then
	allowed_base="$legacy_preview_base"
else
	allowed_base=""
fi
if [ -n "$allowed_base" ] && [ "$asset_base" != "$allowed_base" ]; then
	echo "invalid legacy Desktop asset base: $asset_base" >&2
	exit 1
fi
if [ -z "$allowed_base" ] && [ "$asset_base" != "$github_base" ] && [ "$asset_base" != "$r2_base" ]; then
	echo "invalid official Desktop asset base: $asset_base" >&2
	exit 1
fi
if [ ! -f "$manifest" ]; then
	echo "Desktop release manifest does not exist: $manifest" >&2
	exit 1
fi

jq -e \
	--arg version "$version" \
	--arg notes_version "$notes_version" \
	--arg base "$asset_base" \
	--argjson legacy "$legacy" '
	def exact_keys($expected):
		(type == "object") and ((keys | sort) == ($expected | sort));
	def valid_asset($name):
		(type == "object") and
		(.url == ($base + $name)) and
		(.sig == ($base + $name + ".minisig")) and
			(.size | type == "number" and . > 0 and . <= 1073741824 and floor == .) and
			(.sha256 | type == "string" and test("^[0-9a-f]{64}$"));

	(type == "object") and
	(.version == $version) and
	(.download_page == "https://reasonix.io/?download=desktop#start") and
	(if $legacy
		then (.release_notes_url == null or .release_notes_url == ("https://reasonix.io/changelog/" + $notes_version + "/"))
		else (.release_notes_url == ("https://reasonix.io/changelog/" + $notes_version + "/"))
	end) and
	(.platforms | exact_keys([
		"darwin-arm64",
		"darwin-amd64",
		"windows-amd64",
		"windows-arm64",
		"linux-amd64"
	])) and
	(.platforms["darwin-arm64"] | valid_asset("Reasonix-darwin-arm64.zip")) and
	(.platforms["darwin-amd64"] | valid_asset("Reasonix-darwin-amd64.zip")) and
	(.platforms["windows-amd64"] | valid_asset("Reasonix-windows-amd64-installer.exe")) and
	(.platforms["windows-arm64"] | valid_asset("Reasonix-windows-arm64-installer.exe")) and
	(.platforms["linux-amd64"] | valid_asset("Reasonix-linux-amd64.tar.gz")) and
	(.native_packages | exact_keys(["linux-amd64"])) and
	(.native_packages["linux-amd64"] | valid_asset("Reasonix-linux-amd64.deb")) and
	(if $legacy and (.downloads == null)
		then true
		else
			(.downloads | exact_keys([
				"Reasonix-darwin-universal.dmg",
				"Reasonix-windows-amd64.zip"
			])) and
			(.downloads["Reasonix-darwin-universal.dmg"] | valid_asset("Reasonix-darwin-universal.dmg")) and
			(.downloads["Reasonix-windows-amd64.zip"] | valid_asset("Reasonix-windows-amd64.zip"))
	end)
' "$manifest" >/dev/null
