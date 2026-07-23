#!/usr/bin/env bash
# Resolve tag/version/channel/prerelease for one desktop release run, shared by the
# build, publish, and mirror jobs so they agree on a single value. Reads the run's
# context from env and writes the four outputs to $GITHUB_OUTPUT.
#
#   stable: from a desktop-v* prerelease tag push, a manual dispatch with `tag`,
#           or the stable release orchestrator's workflow_call input.
#   canary: a manual dispatch with channel=canary; version is synthesized from
#           base_version + the monotonic run_number, tag is the rolling
#           `desktop-canary`, and it is always a prerelease.
set -euo pipefail

stable_semver_re='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
release_semver_re='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+)(\.[0-9A-Za-z-]+)*)?$'

if [ "${IN_CHANNEL:-stable}" = "canary" ]; then
	base="${IN_BASE_VERSION:?canary dispatch requires base_version}"
	base="${base#v}"
	base="${base#desktop-v}"
	if [[ ! "$base" =~ $stable_semver_re ]]; then
		echo "::error::canary base version must be MAJOR.MINOR.PATCH, got: $base" >&2
		exit 1
	fi
	version="v${base}-canary.${RUN_NUMBER}"
	tag="desktop-canary"
	channel="canary"
	prerelease="true"
else
	if [ -n "${IN_TAG:-}" ]; then
		tag="${IN_TAG}"
	elif [ "${EVENT_NAME:-}" = "workflow_dispatch" ]; then
		echo "::error::stable dispatch requires tag" >&2
		exit 1
	else
		tag="${REF_NAME:?stable release requires a tag input or ref name}"
	fi
	if [[ ! "$tag" =~ ^desktop-v(.+)$ ]] || [[ ! "${BASH_REMATCH[1]}" =~ $release_semver_re ]]; then
		echo "::error::desktop release tag must be desktop-vMAJOR.MINOR.PATCH[-PRERELEASE], got: $tag" >&2
		exit 1
	fi
	version="${tag#desktop-}"
	channel="stable"
	case "$version" in
	*-*) prerelease="true" ;;
	*) prerelease="false" ;;
	esac
fi

{
	echo "tag=$tag"
	echo "version=$version"
	echo "channel=$channel"
	echo "prerelease=$prerelease"
} >>"$GITHUB_OUTPUT"

echo "resolved: tag=$tag version=$version channel=$channel prerelease=$prerelease"
