#!/usr/bin/env bash
# Resolve and validate one native CLI release tag. Manual recovery exposes only
# official versions; prerelease parsing remains for historical workflow calls.
set -euo pipefail

stable_semver_re='(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)'
release_semver_re="^v${stable_semver_re}(-([0-9A-Za-z-]+)(\.[0-9A-Za-z-]+)*)?)?$"
preview_semver_re="^v${stable_semver_re}-preview\.(0|[1-9][0-9]*)$"

if [ "${IN_ORCHESTRATED:-false}" = "true" ]; then
	tag="${IN_TAG:?orchestrated CLI release requires tag}"
else
	if [ "${EVENT_NAME:-}" = "workflow_dispatch" ]; then
		if [ "${CALLER_REF:-}" != "refs/heads/main-v2" ] || [ "${CALLER_REF_PROTECTED:-}" != "true" ]; then
			echo "::error::manual CLI releases must run from protected main-v2" >&2
			exit 1
		fi
		tag="${IN_TAG:?manual CLI release requires an existing protected tag}"
	else
		tag="${REF_NAME:?CLI tag push requires ref name}"
	fi
fi

if [[ ! "$tag" =~ $release_semver_re ]]; then
	echo "::error::CLI release tag must be vMAJOR.MINOR.PATCH[-PRERELEASE], got: $tag" >&2
	exit 1
fi

base_version="${BASH_REMATCH[1]}.${BASH_REMATCH[2]}.${BASH_REMATCH[3]}"
version="${tag#v}"
case "$version" in
*-*) prerelease="true" ;;
*) prerelease="false" ;;
esac

if [ "${EVENT_NAME:-}" = "workflow_dispatch" ] && \
	[ "${IN_ORCHESTRATED:-false}" != "true" ] && [ "$prerelease" = "true" ]; then
	echo "::error::manual CLI recovery accepts only vMAJOR.MINOR.PATCH" >&2
	exit 1
fi

if [[ "$tag" =~ $preview_semver_re ]]; then
	channel="preview"
	notes_version="$tag"
else
	case "$tag" in
	v*-preview*)
		echo "::error::CLI Preview tag must be vMAJOR.MINOR.PATCH-preview.N, got: $tag" >&2
		exit 1
		;;
	esac
	channel="stable"
	case "$version" in
	*-*) notes_version="v${base_version}" ;;
	*) notes_version="$tag" ;;
	esac
fi

requested_channel="${IN_CHANNEL:-}"
if [ -n "$requested_channel" ]; then
	case "$requested_channel" in
	stable | preview) ;;
	*)
		echo "::error::CLI release channel must be stable or preview, got: $requested_channel" >&2
		exit 1
		;;
	esac
	if [ "$requested_channel" != "$channel" ]; then
		echo "::error::CLI tag $tag belongs to $channel, not requested channel $requested_channel" >&2
		exit 1
	fi
fi

if [ "$channel" = "preview" ] && [ "${IN_ORCHESTRATED:-false}" != "true" ]; then
	echo "::error::public CLI Preview releases must be dispatched by release-preview.yml" >&2
	exit 1
fi

{
	echo "tag=$tag"
	echo "version=$version"
	echo "base_version=$base_version"
	echo "notes_version=$notes_version"
	echo "channel=$channel"
	echo "prerelease=$prerelease"
} >>"${GITHUB_OUTPUT:-/dev/stdout}"

echo "CLI release resolved: tag=$tag channel=$channel prerelease=$prerelease"
