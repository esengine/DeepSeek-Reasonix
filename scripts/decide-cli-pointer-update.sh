#!/usr/bin/env bash
set -euo pipefail

# Decide whether a validated CLI release manifest may and should replace one
# public channel pointer. Newer tags advance the pointer, older tags never do,
# and equal tags repair the pointer when its exact metadata differs.

if [ "$#" -ne 3 ]; then
	echo "usage: $0 CHANNEL CANDIDATE_MANIFEST CURRENT_MANIFEST|-" >&2
	exit 2
fi

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
compare="$script_dir/compare-cli-release-tags.sh"
channel="$1"
candidate_file="$2"
current_file="$3"

case "$channel" in
	stable | preview) ;;
	*)
		echo "unsupported CLI pointer channel: $channel" >&2
		exit 2
		;;
esac
if [ ! -f "$candidate_file" ]; then
	echo "candidate CLI manifest is not a readable file: $candidate_file" >&2
	exit 2
fi

candidate_tag="$(jq -er '.tag_name | strings' "$candidate_file")"
bash "$compare" "$channel" "$candidate_tag" "" >/dev/null
if [ "$current_file" = "-" ]; then
	echo update
	exit 0
fi
if [ ! -f "$current_file" ]; then
	echo "current CLI manifest is not a readable file: $current_file" >&2
	exit 2
fi

current_tag="$(jq -er '.tag_name | strings' "$current_file")"
decision="$(bash "$compare" "$channel" "$candidate_tag" "$current_tag")"
if [ "$current_tag" != "$candidate_tag" ] && [ "$decision" = "skip" ]; then
	echo skip
elif cmp -s "$candidate_file" "$current_file"; then
	echo skip
else
	echo update
fi
