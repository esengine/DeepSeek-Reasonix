#!/usr/bin/env bash
# Single owner of the temporary unsigned manual-download Desktop exception.
#
# The exception removes only Windows Authenticode/SignPath. minisign, SHA-256,
# artifact identity, native builds, and startup verification always stay on, and
# no Desktop update entry point may advance while it is active.
#
# A row carries a candidate SHA only when its tag was already immutable at
# approval time. Those rows are historical recoveries and must run with
# allow_recovery=true. A row approved before its candidate exists carries no
# SHA, so it must keep the normal candidate and push-CI validation instead of
# trading that validation away for the signing exception.
set -euo pipefail

# Approved tag -> pinned candidate SHA ("" when the candidate is validated by
# the normal stable path). Adding a row is the authorization boundary; every
# caller reads it from here so an exception cannot drift between surfaces.
manual_desktop_exception_sha() {
	case "$1" in
	desktop-v1.38.8) printf '%s' 7278072720a2dc7a31cce0eec18c1eacc149c0e0 ;;
	desktop-v1.38.9) printf '%s' dc915ab97bfdeb5d0e414916c0f58708dca80051 ;;
	*) return 1 ;;
	esac
}

# validate DESKTOP_TAG [CANDIDATE_SHA] [ALLOW_RECOVERY]
# Empty optional arguments are not checked, so a caller that cannot observe a
# value never weakens the rule for a caller that can.
manual_desktop_exception_validate() {
	local tag="${1:-}" sha="${2:-}" allow_recovery="${3:-}" pinned

	if ! pinned="$(manual_desktop_exception_sha "$tag")"; then
		echo "manual Desktop exception is not approved for tag: ${tag:-<empty>}" >&2
		return 1
	fi

	if [ -n "$pinned" ]; then
		if [ -n "$sha" ] && [ "$sha" != "$pinned" ]; then
			echo "manual Desktop exception for $tag is pinned to candidate $pinned, got $sha" >&2
			return 1
		fi
		if [ -n "$allow_recovery" ] && [ "$allow_recovery" != "true" ]; then
			echo "manual Desktop exception for $tag is an immutable recovery and requires allow_recovery=true" >&2
			return 1
		fi
		return 0
	fi

	if [ "$allow_recovery" = "true" ]; then
		echo "manual Desktop exception for $tag must keep normal candidate validation (allow_recovery=false)" >&2
		return 1
	fi
	return 0
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
	case "${1:-}" in
	validate)
		shift
		manual_desktop_exception_validate "$@"
		;;
	*)
		echo "usage: $0 validate DESKTOP_TAG [CANDIDATE_SHA] [ALLOW_RECOVERY]" >&2
		exit 2
		;;
	esac
fi
