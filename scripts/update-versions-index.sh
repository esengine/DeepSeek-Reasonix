#!/usr/bin/env bash
# Merge one published release into the rollback index and print the result.
#
# The index is what lets a user go back: it lists which versions were ever
# public and where each one's immutable manifest lives. It deliberately does NOT
# copy asset URLs or signatures — an entry points at <tag>/latest.json, so a
# rollback download verifies through exactly the same path as a forward update
# and there is never a second source of truth for what an artifact should be.
#
# Merging is additive: an existing entry for the same version wins, so a rerun
# or a recovery publication can never rewrite history. Usage:
#
#   update-versions-index.sh <existing-index|-> <version> <tag> <channel> <published-at> [keep]
set -euo pipefail

if [ "$#" -lt 5 ] || [ "$#" -gt 6 ]; then
  echo "usage: $0 <existing-index|-> <version> <tag> <channel> <published-at> [keep]" >&2
  exit 2
fi

existing="$1"
version="$2"
tag="$3"
channel="$4"
published_at="$5"
keep="${6:-20}"

for value in "$version" "$tag" "$channel" "$published_at"; do
  if [ -z "$value" ]; then
    echo "::error::versions index requires version, tag, channel and published-at" >&2
    exit 1
  fi
done
case "$keep" in
  ''|*[!0-9]*)
    echo "::error::keep must be a positive integer, got $keep" >&2
    exit 1
    ;;
esac
if [ "$keep" -lt 1 ]; then
  echo "::error::keep must be at least 1" >&2
  exit 1
fi

base="{\"schemaVersion\":1,\"versions\":[]}"
if [ "$existing" != "-" ]; then
  if [ ! -f "$existing" ]; then
    echo "::error::existing index $existing not found" >&2
    exit 1
  fi
  # A corrupt index must fail the release rather than silently reset the list:
  # losing history is exactly the failure this file exists to prevent.
  if ! jq -e '.versions | type == "array"' "$existing" >/dev/null; then
    echo "::error::existing index $existing is not a versions index" >&2
    exit 1
  fi
  base="$(cat "$existing")"
fi

printf '%s' "$base" | jq \
  --arg version "$version" \
  --arg tag "$tag" \
  --arg channel "$channel" \
  --arg publishedAt "$published_at" \
  --argjson keep "$keep" '
  def semver: [splits("[.-]")] | map(select(test("^[0-9]+$")) | tonumber);
  {
    schemaVersion: 1,
    updatedAt: $publishedAt,
    versions: (
      (.versions // [])
      + [{
          version: $version,
          tag: $tag,
          channel: $channel,
          publishedAt: $publishedAt,
          manifest: ("https://dl.reasonix.io/" + $tag + "/latest.json"),
        }]
      # Existing entries win: unique_by keeps the first of each group, so a
      # rerun cannot rewrite what was already published under that version.
      | unique_by(.version)
      | sort_by(.version | semver)
      | reverse
      | .[0:$keep]
    ),
  }
'
