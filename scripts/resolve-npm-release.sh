#!/usr/bin/env bash
# Resolve and validate the version argument passed to npm/build.mjs. Keeping
# workflow inputs in environment variables avoids interpolating dispatch input
# into shell source.
set -euo pipefail

stable_semver_re='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
release_semver_re='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+)(\.[0-9A-Za-z-]+)*)?$'

if [ "${EVENT_NAME:-}" = "workflow_dispatch" ] || [ "${IN_ORCHESTRATED:-false}" = "true" ]; then
	base="${IN_BASE_VERSION:?dispatch/call requires base_version}"
	base="${base#v}"
	if [[ ! "$base" =~ $stable_semver_re ]]; then
		echo "::error::base version must be MAJOR.MINOR.PATCH, got: $base" >&2
		exit 1
	fi
	if [ "${IN_CHANNEL:-stable}" = "canary" ]; then
		arg="v${base}-canary.${RUN_NUMBER:?canary requires run number}"
	else
		tag="${IN_TAG:-}"
		if [ -z "$tag" ]; then
			echo "::error::stable publication requires an existing npm tag" >&2
			exit 1
		fi
		if [ "$tag" != "npm-v${base}" ]; then
			echo "::error::tag $tag does not match requested version npm-v${base}" >&2
			exit 1
		fi
		arg="v${base}"
	fi
else
	tag="${REF_NAME:?tag push requires ref name}"
	if [[ ! "$tag" =~ ^npm-v(.+)$ ]] || [[ ! "${BASH_REMATCH[1]}" =~ $release_semver_re ]]; then
		echo "::error::npm release tag must be npm-vMAJOR.MINOR.PATCH[-PRERELEASE], got: $tag" >&2
		exit 1
	fi
	arg="$tag"
fi

echo "arg=$arg" >>"${GITHUB_OUTPUT:-/dev/stdout}"
echo "npm release resolved: $arg"
