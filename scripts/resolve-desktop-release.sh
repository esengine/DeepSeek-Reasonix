#!/usr/bin/env bash
# Resolve tag/version/channel/prerelease for one desktop release run, shared by the
# build, publish, and mirror jobs so they agree on a single value. Reads the run's
# context from env and writes the four outputs to $GITHUB_OUTPUT.
#
#   stable: from a desktop-v* tag push, or a manual dispatch with `tag`.
#   canary: a manual dispatch with channel=canary; version is synthesized from
#           base_version + the monotonic run_number, tag is the rolling
#           `desktop-canary`, and it is always a prerelease.
set -euo pipefail

if [ "${EVENT_NAME:-}" = "workflow_dispatch" ] && [ "${IN_CHANNEL:-stable}" = "canary" ]; then
	base="${IN_BASE_VERSION:?canary dispatch requires base_version}"
	base="${base#v}"
	base="${base#desktop-v}"
	version="v${base}-canary.${RUN_NUMBER}"
	tag="desktop-canary"
	channel="canary"
	prerelease="true"
else
	if [ "${EVENT_NAME:-}" = "workflow_dispatch" ]; then
		tag="${IN_TAG:?stable dispatch requires tag}"
	else
		tag="${REF_NAME}"
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
