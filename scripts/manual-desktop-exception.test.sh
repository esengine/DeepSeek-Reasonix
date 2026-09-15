#!/usr/bin/env bash
# The manual Desktop exception removes Windows Authenticode only. It must never
# also buy an unapproved tag, a different candidate, or a skipped candidate
# validation, so every rule is asserted against the one owning script.
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
owner="$repo_root/scripts/manual-desktop-exception.sh"

accepts() {
	if ! bash "$owner" validate "$@" 2>/dev/null; then
		echo "manual Desktop exception rejected an approved input: $*" >&2
		exit 1
	fi
}

rejects() {
	if bash "$owner" validate "$@" 2>/dev/null; then
		echo "manual Desktop exception accepted a forbidden input: $*" >&2
		exit 1
	fi
}

# Only listed tags carry the exception.
rejects
rejects ""
rejects desktop-v1.39.0 "" false
rejects v1.38.9 "" false
rejects desktop-v1.38.10 "" false

# Every published row stays bound to its own immutable candidate, and further
# runs against it are recoveries rather than fresh releases.
while read -r tag sha; do
	accepts "$tag" "$sha" true
	accepts "$tag"
	rejects "$tag" "$sha" false
	rejects "$tag" 0000000000000000000000000000000000000000 true
done <<'ROWS'
desktop-v1.38.8 7278072720a2dc7a31cce0eec18c1eacc149c0e0
desktop-v1.38.9 dc915ab97bfdeb5d0e414916c0f58708dca80051
ROWS

# A row approved before its candidate exists carries no SHA. It must keep the
# normal candidate and push-CI validation, which release-stable.yml runs only
# when allow_recovery is false; trading that away would make the signing
# exception a release bypass. Exercise that path with an injected row so it
# stays covered once every real row has been published and pinned.
# shellcheck source=/dev/null
. "$owner"
manual_desktop_exception_sha() {
	case "$1" in
	desktop-v9.9.9) printf '' ;;
	*) return 1 ;;
	esac
}
manual_desktop_exception_validate desktop-v9.9.9 any-candidate-sha false ||
	{ echo "unpinned row rejected under allow_recovery=false" >&2; exit 1; }
manual_desktop_exception_validate desktop-v9.9.9 ||
	{ echo "unpinned row rejected without optional arguments" >&2; exit 1; }
if manual_desktop_exception_validate desktop-v9.9.9 any-candidate-sha true 2>/dev/null; then
	echo "unpinned row accepted allow_recovery=true, which skips candidate validation" >&2
	exit 1
fi
if manual_desktop_exception_validate desktop-v1.38.8 "" false 2>/dev/null; then
	echo "injected allowlist did not replace the real rows" >&2
	exit 1
fi

echo "manual Desktop exception contract ok"
