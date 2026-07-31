#!/usr/bin/env bash
# Resolve tag/version/channel/prerelease for one desktop release run, shared by the
# build, publish, and mirror jobs so they agree on a single value. Reads the run's
# context from env and writes the four outputs to $GITHUB_OUTPUT.
#
#   stable: from a desktop-v* prerelease tag push, a manual dispatch with `tag`,
#           or the stable release orchestrator's workflow_call input.
#   preview: public publication requires the approved Preview orchestrator;
#            standalone Preview is limited to non-publishing signing checks.
#            Legacy canary input is accepted for those checks. The version uses
#            the orchestrator ordinal or, for a signing check, the run number.
set -euo pipefail

stable_semver_re='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
release_semver_re='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-([0-9A-Za-z-]+)(\.[0-9A-Za-z-]+)*)?$'
preview_semver_re='^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-preview\.(0|[1-9][0-9]*)$'

if [ "${IN_CHANNEL:-stable}" = "preview" ] || [ "${IN_CHANNEL:-stable}" = "canary" ]; then
	if [ "${IN_ORCHESTRATED:-false}" != "true" ] && \
		[ "${IN_PRODUCTION_SIGNING_SMOKE:-false}" != "true" ] && \
		[ "${IN_SIGNING_PREFLIGHT:-false}" != "true" ]; then
		echo "::error::public Desktop Preview releases must be dispatched by release-preview.yml" >&2
		exit 1
	fi
	if [ -n "${IN_TAG:-}" ]; then
		echo "::error::Desktop Preview versions are synthesized from protected main-v2; tag must be empty" >&2
		exit 1
	fi
	base="${IN_BASE_VERSION:?preview dispatch requires base_version}"
	base="${base#v}"
	base="${base#desktop-v}"
	if [[ ! "$base" =~ $stable_semver_re ]]; then
		echo "::error::preview base version must be MAJOR.MINOR.PATCH, got: $base" >&2
		exit 1
	fi
	preview_number="${IN_PREVIEW_NUMBER:-${RUN_NUMBER:-}}"
	if [[ ! "$preview_number" =~ ^(0|[1-9][0-9]*)$ ]]; then
		echo "::error::preview number must be a non-negative integer, got: $preview_number" >&2
		exit 1
	fi
	version="v${base}-preview.${preview_number}"
	tag="desktop-${version}"
	channel="preview"
	prerelease="true"
	notes_version="$version"
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
	if [[ "${version#v}" =~ $preview_semver_re ]]; then
		echo "::error::Desktop Preview versions must use channel=preview without a Git tag, got: $tag" >&2
		exit 1
	fi
	channel="stable"
	case "$version" in
	*-*)
		prerelease="true"
		notes_version="${version%%-*}"
		;;
	*)
		prerelease="false"
		notes_version="$version"
		;;
	esac
fi

{
	echo "tag=$tag"
	echo "version=$version"
	echo "channel=$channel"
	echo "prerelease=$prerelease"
	echo "notes_version=$notes_version"
} >>"$GITHUB_OUTPUT"

echo "resolved: tag=$tag version=$version channel=$channel prerelease=$prerelease"
