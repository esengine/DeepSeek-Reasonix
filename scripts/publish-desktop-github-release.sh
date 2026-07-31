#!/usr/bin/env bash
# Create or recover one immutable Desktop GitHub release. Existing metadata and
# assets must match the candidate; interrupted runs may only fill missing assets.
set -euo pipefail

if [ "$#" -ne 5 ]; then
	echo "usage: $0 TAG VERSION PRERELEASE NOTES_FILE ASSET_DIRECTORY" >&2
	exit 2
fi

tag="$1"
version="$2"
prerelease="$3"
notes_file="$4"
asset_dir="${5%/}"
repository="${GITHUB_REPOSITORY:?GITHUB_REPOSITORY is required}"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
directory_verifier="$script_dir/verify-desktop-release-directory.sh"
title="Reasonix Desktop $version"

case "$prerelease" in
true | false) ;;
*)
	echo "Desktop GitHub release prerelease flag must be true or false: $prerelease" >&2
	exit 2
	;;
esac
if [ ! -s "$notes_file" ]; then
	echo "Desktop GitHub release notes are missing or empty: $notes_file" >&2
	exit 2
fi
if [ ! -d "$asset_dir" ]; then
	echo "Desktop GitHub release asset directory is missing: $asset_dir" >&2
	exit 2
fi
if find "$asset_dir" -mindepth 2 -print -quit | grep -q .; then
	echo "Desktop GitHub release assets must be top-level files" >&2
	exit 2
fi
if ! find "$asset_dir" -mindepth 1 -maxdepth 1 -type f -print -quit | grep -q .; then
	echo "Desktop GitHub release asset directory is empty: $asset_dir" >&2
	exit 2
fi

work_dir="$(mktemp -d)"
cleanup() {
	case "$work_dir" in
	*/tmp.* | */reasonix-desktop-release.*) rm -rf -- "$work_dir" ;;
	*) echo "refusing to clean unexpected Desktop release temp directory: $work_dir" >&2 ;;
	esac
}
trap cleanup EXIT

release_json="$work_dir/release.json"
release_error="$work_dir/release.error"
release_endpoint="repos/${repository}/releases/tags/${tag}"
if ! gh api "$release_endpoint" >"$release_json" 2>"$release_error"; then
	if ! grep -Eiq '404|Not Found' "$release_error"; then
		cat "$release_error" >&2
		exit 1
	fi
	args=(--title "$title" --notes-file "$notes_file")
	if [ "$prerelease" = "true" ]; then
		args+=(--prerelease --latest=false)
	else
		args+=(--latest)
	fi
	gh release create "$tag" -R "$repository" "${args[@]}"
	gh api "$release_endpoint" >"$release_json"
fi

jq -e \
	--arg tag "$tag" \
	--arg title "$title" \
	--rawfile body "$notes_file" \
	--argjson prerelease "$prerelease" '
	(type == "object") and
	(.tag_name == $tag) and
	(.name == $title) and
	(.body == $body) and
	(.draft == false) and
	(.prerelease == $prerelease) and
	(.assets | type == "array") and
	((.assets | map(.name) | length) == (.assets | map(.name) | unique | length))
' "$release_json" >/dev/null

existing_dir="$work_dir/existing"
mkdir -p "$existing_dir"
while IFS= read -r name; do
	if [ -z "$name" ] || [ ! -f "$asset_dir/$name" ]; then
		echo "Desktop GitHub release has unexpected asset: ${name:-<empty>}" >&2
		exit 1
	fi
	gh release download "$tag" -R "$repository" --pattern "$name" --dir "$existing_dir"
done < <(jq -r '.assets[].name' "$release_json")

bash "$directory_verifier" --allow-missing "$asset_dir" "$existing_dir"

while IFS= read -r -d '' asset; do
	name="$(basename "$asset")"
	if [ ! -f "$existing_dir/$name" ]; then
		gh release upload "$tag" -R "$repository" "$asset"
	fi
done < <(find "$asset_dir" -mindepth 1 -maxdepth 1 -type f -print0)

published_dir="$work_dir/published"
mkdir -p "$published_dir"
gh release download "$tag" -R "$repository" --dir "$published_dir"
bash "$directory_verifier" "$asset_dir" "$published_dir"
echo "Desktop GitHub release verified: tag=$tag assets=$(find "$asset_dir" -mindepth 1 -maxdepth 1 -type f | wc -l | tr -d ' ')"
