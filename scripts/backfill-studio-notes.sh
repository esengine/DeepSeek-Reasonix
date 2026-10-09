#!/usr/bin/env bash
# One-time backfill of release notes for the versions already in the Studio
# catalog: upload studio/notes/<version>.md for each entry without a notes field,
# then add the field to those entries. A version whose tag carries no notes file
# keeps no field. Objects already in the bucket are never rewritten.
#
# Dry-run unless --apply is given. Needs R2_ACCOUNT_ID, R2_BUCKET and the AWS
# credentials of the mirror in the environment, and GH_TOKEN (or gh login) for the
# credit lookups the renderer makes. Run it from a clone that has the studio tags.
#
#   backfill-studio-notes.sh [--apply]
set -euo pipefail

apply=false
case "${1:-}" in
  "") ;;
  --apply) apply=true ;;
  *) echo "usage: $0 [--apply]" >&2; exit 2 ;;
esac
: "${R2_ACCOUNT_ID:?R2_ACCOUNT_ID is required}"
: "${R2_BUCKET:?R2_BUCKET is required}"

here="$(cd "$(dirname "$0")" && pwd)"
repo="${BACKFILL_REPO:-$(cd "$here/.." && pwd)}"
endpoint="https://${R2_ACCOUNT_ID}.r2.cloudflarestorage.com"
work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
base="https://dl.reasonix.io/studio/notes"

idle() {
  local busy
  busy="$(gh run list --workflow release-studio.yml --limit 30 --json status --jq '[.[] | select(.status != "completed")] | length')"
  [ "$busy" = "0" ]
}

fetch_catalog() {
  aws s3 cp "s3://${R2_BUCKET}/studio/versions.json" "$1" --endpoint-url "$endpoint" >/dev/null
  jq -e '.versions | type == "array"' "$1" >/dev/null
}

has_object() {
  aws s3api head-object --bucket "$R2_BUCKET" --key "studio/notes/$1.md" --endpoint-url "$endpoint" >/dev/null 2>&1
}

if $apply && ! idle; then
  echo "refusing: a release-studio.yml run is queued or in progress; its catalog write would race this one" >&2
  exit 1
fi

fetch_catalog "$work/catalog.json"
have=()
while IFS= read -r version; do
  v="${version#v}"
  if has_object "$v"; then
    echo "exists   $v"
    have+=("$v")
    continue
  fi
  if ! git -C "$repo" show "studio-v$v:release-notes/studio/$v.md" > "$work/$v.src.md" 2>/dev/null; then
    echo "no notes $v"
    continue
  fi
  if ! $apply; then
    echo "would upload $v"
    have+=("$v")
    continue
  fi
  node "$here/studio-release-notes.mjs" "$work/$v.src.md" "$work/$v.md"
  aws s3 cp "$work/$v.md" "s3://${R2_BUCKET}/studio/notes/$v.md" --endpoint-url "$endpoint" \
    --content-type "text/markdown; charset=utf-8" \
    --cache-control "public, max-age=31536000, immutable" >/dev/null
  has_object "$v"
  echo "uploaded $v"
  have+=("$v")
done < <(jq -r '.versions[] | select((.notes // "") == "") | .version' "$work/catalog.json")

if ! $apply; then
  echo "dry run: ${#have[@]} entries would carry notes; rerun with --apply"
  exit 0
fi

# Objects first, catalog last. Read the catalog again so a release that landed
# while the notes were rendering is kept, and stop if one is running now.
idle || { echo "refusing to write the catalog: a release run started" >&2; exit 1; }
fetch_catalog "$work/catalog.json"
printf '%s\n' "${have[@]:-}" | jq -R . | jq -s 'map(select(. != ""))' > "$work/have.json"
jq --slurpfile have "$work/have.json" --arg base "$base" '
  .versions |= map(
    (.version | ltrimstr("v")) as $n
    | if (.notes // "") == "" and ($have[0] | index($n) != null)
      then . + {notes: ($base + "/" + $n + ".md")}
      else . end)' "$work/catalog.json" > "$work/next.json"
jq -e '.versions | length > 0' "$work/next.json" >/dev/null
aws s3 cp "$work/next.json" "s3://${R2_BUCKET}/studio/versions.json" --endpoint-url "$endpoint" \
  --content-type "application/json; charset=utf-8" \
  --cache-control "public, max-age=300, stale-if-error=86400" >/dev/null
echo "catalog updated: ${#have[@]} entries carry notes"
