#!/usr/bin/env bash
set -euo pipefail

allow_missing=false
allow_legacy_manifest=false
while [ "$#" -gt 0 ]; do
	case "$1" in
	--allow-missing)
		allow_missing=true
		shift
		;;
	--allow-legacy-manifest)
		allow_legacy_manifest=true
		shift
		;;
	*) break ;;
	esac
done
if [ "$#" -ne 2 ]; then
	echo "usage: $0 [--allow-missing] [--allow-legacy-manifest] CANDIDATE_DIRECTORY EXISTING_DIRECTORY" >&2
	exit 2
fi

candidate_dir="${1%/}"
existing_dir="${2%/}"
script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [ ! -d "$candidate_dir" ] || [ ! -d "$existing_dir" ]; then
	echo "Desktop release comparison requires two directories" >&2
	exit 2
fi

verify_subset() {
	local source_dir="$1"
	local target_dir="$2"
	local source_file relative target_file
	while IFS= read -r -d '' source_file; do
		relative="${source_file#"$source_dir"/}"
		target_file="$target_dir/$relative"
		if [ ! -f "$target_file" ]; then
			echo "Desktop release directory is missing $relative" >&2
			return 1
		fi
		if [ "$allow_legacy_manifest" = "true" ] && [ "$relative" = "latest.json" ]; then
			if ! bash "$script_dir/compare-desktop-release-manifests.sh" \
				"$candidate_dir/latest.json" "$existing_dir/latest.json"; then
				echo "Desktop release directory has conflicting content for $relative" >&2
				return 1
			fi
			continue
		fi
		if ! cmp -s "$source_file" "$target_file"; then
			echo "Desktop release directory has conflicting content for $relative" >&2
			return 1
		fi
	done < <(find "$source_dir" -type f -print0)
}

# Existing objects may never disagree with or fall outside the candidate set.
verify_subset "$existing_dir" "$candidate_dir"
if [ "$allow_missing" != "true" ]; then
	verify_subset "$candidate_dir" "$existing_dir"
fi
