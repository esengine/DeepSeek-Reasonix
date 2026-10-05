#!/usr/bin/env bash
set -euo pipefail

candidate="${1:-}"
existing="${2:-}"

for manifest in "$candidate" "$existing"; do
	if [ ! -f "$manifest" ]; then
		echo "CLI release manifest does not exist: $manifest" >&2
		exit 1
	fi
done

# Immutable manifests published before release_notes_url existed remain valid.
# Normalize only that missing field from the strict candidate; every other
# semantic difference must still fail the immutable recovery check.
jq -e -s '
	.[0] as $candidate |
	.[1] as $existing |
	($existing | .release_notes_url = (.release_notes_url // $candidate.release_notes_url)) == $candidate
' "$candidate" "$existing" >/dev/null
