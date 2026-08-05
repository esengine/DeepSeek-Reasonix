#!/usr/bin/env bash
set -euo pipefail

candidate="${1:-}"
existing="${2:-}"

for manifest in "$candidate" "$existing"; do
	if [ ! -f "$manifest" ]; then
		echo "Desktop release manifest does not exist: $manifest" >&2
		exit 1
	fi
done

# Preserve immutable manifests created before release_notes_url existed. Only
# that absent field inherits the strict candidate value; all other manifest
# content must remain semantically identical.
jq -e -s '
	.[0] as $candidate |
	.[1] as $existing |
	($existing | .release_notes_url = (.release_notes_url // $candidate.release_notes_url)) == $candidate
' "$candidate" "$existing" >/dev/null
