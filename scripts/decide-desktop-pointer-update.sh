#!/usr/bin/env bash
set -euo pipefail

# Select the validated manifest that should align one Desktop channel's public
# pointer(s). The candidate is authoritative when it is newest. Otherwise the
# newest existing pointer repairs lagging compatibility aliases. Equal newer
# versions with different content are ambiguous and fail closed.

if [ "$#" -lt 3 ]; then
	echo "usage: $0 CHANNEL CANDIDATE_MANIFEST CURRENT_MANIFEST|- [CURRENT_MANIFEST|- ...]" >&2
	exit 2
fi

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
compare="$script_dir/compare-desktop-release-versions.sh"
channel="$1"
candidate_file="$2"
shift 2

case "$channel" in
	stable | preview) ;;
	*)
		echo "unsupported Desktop pointer channel: $channel" >&2
		exit 2
		;;
esac
if [ ! -f "$candidate_file" ]; then
	echo "candidate Desktop manifest is not a readable file: $candidate_file" >&2
	exit 2
fi

candidate_version="$(jq -er '.version | strings' "$candidate_file")"
bash "$compare" "$channel" "$candidate_version" "" >/dev/null
selected_file="$candidate_file"
selected_version="$candidate_version"
selected_is_candidate=1

for current_file in "$@"; do
	if [ "$current_file" = "-" ]; then
		continue
	fi
	if [ ! -f "$current_file" ]; then
		echo "current Desktop manifest is not a readable file: $current_file" >&2
		exit 2
	fi

	current_version="$(jq -er '.version | strings' "$current_file")"
	if [ "$(bash "$compare" "$channel" "$current_version" "$selected_version")" = "update" ]; then
		selected_file="$current_file"
		selected_version="$current_version"
		selected_is_candidate=0
	elif [ "$current_version" = "$selected_version" ] &&
		[ "$selected_is_candidate" -eq 0 ] &&
		! cmp -s "$current_file" "$selected_file"; then
		echo "conflicting Desktop $channel pointers have the same newer version $current_version" >&2
		exit 1
	fi
done

needs_update=0
for current_file in "$@"; do
	if [ "$current_file" = "-" ] || ! cmp -s "$selected_file" "$current_file"; then
		needs_update=1
	fi
done

if [ "$needs_update" -eq 1 ]; then
	printf 'update\t%s\n' "$selected_file"
else
	echo skip
fi
