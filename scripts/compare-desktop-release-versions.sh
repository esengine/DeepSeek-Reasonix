#!/usr/bin/env bash
set -euo pipefail

# Compare two public Desktop versions in one release channel. The result is:
#   update - candidate is newer, or the channel pointer does not exist yet
#   skip   - candidate is equal to or older than the current pointer
#
# Decimal components are compared without shell arithmetic so arbitrarily large
# canonical components retain their exact ordering.

export LC_ALL=C

channel="${1:-}"
candidate="${2:-}"
current="${3:-}"

case "$channel" in
	stable)
		version_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
		;;
	preview)
		version_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-preview\.(0|[1-9][0-9]*)$'
		;;
	*)
		echo "unsupported Desktop release channel: $channel" >&2
		exit 2
		;;
esac

parse_version() {
	local value="$1"
	if [[ "$value" =~ $version_pattern ]]; then
		if [ "$channel" = "stable" ]; then
			printf '%s %s %s\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" "${BASH_REMATCH[3]}"
		else
			printf '%s %s %s %s\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" "${BASH_REMATCH[3]}" "${BASH_REMATCH[4]}"
		fi
		return 0
	fi
	echo "invalid $channel Desktop release version: $value" >&2
	return 1
}

candidate_parts="$(parse_version "$candidate")"
if [ -z "$current" ]; then
	echo update
	exit 0
fi
current_parts="$(parse_version "$current")"

candidate_is_newer=0
read -r -a candidate_values <<<"$candidate_parts"
read -r -a current_values <<<"$current_parts"
for i in "${!candidate_values[@]}"; do
	candidate_value="${candidate_values[$i]}"
	current_value="${current_values[$i]}"
	if [ "${#candidate_value}" -gt "${#current_value}" ] ||
		{ [ "${#candidate_value}" -eq "${#current_value}" ] && [[ "$candidate_value" > "$current_value" ]]; }; then
		candidate_is_newer=1
		break
	fi
	if [ "${#candidate_value}" -lt "${#current_value}" ] ||
		{ [ "${#candidate_value}" -eq "${#current_value}" ] && [[ "$candidate_value" < "$current_value" ]]; }; then
		break
	fi
done

if [ "$candidate_is_newer" -eq 1 ]; then
	echo update
else
	echo skip
fi
