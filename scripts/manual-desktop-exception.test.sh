#!/usr/bin/env bash
# The manual Desktop exception removes Windows Authenticode only. It must never
# also buy an unapproved tag, a different candidate, or a skipped candidate
# validation, so every rule is asserted against the one owning script.
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
owner="$repo_root/scripts/manual-desktop-exception.sh"
approved_sha=7278072720a2dc7a31cce0eec18c1eacc149c0e0

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

# An immutable row stays bound to its candidate and to recovery mode.
accepts desktop-v1.38.8 "$approved_sha" true
rejects desktop-v1.38.8 "$approved_sha" false
rejects desktop-v1.38.8 0000000000000000000000000000000000000000 true

# A row approved before its candidate exists keeps the normal candidate and
# push-CI validation, which release-stable.yml runs only when allow_recovery is
# false. Trading that away would make the signing exception a release bypass.
accepts desktop-v1.38.9 any-candidate-sha false
rejects desktop-v1.38.9 any-candidate-sha true

# Callers that cannot observe a value must not weaken it for callers that can.
accepts desktop-v1.38.8
accepts desktop-v1.38.9

echo "manual Desktop exception contract ok"
