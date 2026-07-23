#!/usr/bin/env bash
# Verify the non-forgeable GitHub context behind an orchestrated stable release.
# Inputs arrive through environment variables so workflow expressions never
# become shell source.
set -euo pipefail

actual_caller="${ACTUAL_CALLER_WORKFLOW_REF:?ACTUAL_CALLER_WORKFLOW_REF is required}"
expected_caller="${EXPECTED_CALLER_WORKFLOW_REF:?EXPECTED_CALLER_WORKFLOW_REF is required}"
caller_event="${CALLER_EVENT_NAME:?CALLER_EVENT_NAME is required}"
caller_ref="${CALLER_REF:?CALLER_REF is required}"
caller_ref_protected="${CALLER_REF_PROTECTED:?CALLER_REF_PROTECTED is required}"
caller_workflow_sha="${CALLER_WORKFLOW_SHA:?CALLER_WORKFLOW_SHA is required}"
caller_sha="${CALLER_SHA:?CALLER_SHA is required}"
approved_cli_tag="${APPROVED_CLI_TAG:?APPROVED_CLI_TAG is required}"
approved_sha="${APPROVED_SHA:?APPROVED_SHA is required}"

if [ "$actual_caller" != "$expected_caller" ]; then
	echo "::error::orchestrated release caller is $actual_caller, expected $expected_caller" >&2
	exit 1
fi
if [ "$caller_ref_protected" != "true" ]; then
	echo "::error::orchestrated release caller ref is not protected: $caller_ref" >&2
	exit 1
fi
if [[ ! "$approved_cli_tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
	echo "::error::approved CLI tag must be vMAJOR.MINOR.PATCH, got: $approved_cli_tag" >&2
	exit 1
fi

case "$caller_event" in
push)
	expected_ref="refs/tags/$approved_cli_tag"
	if [ "$caller_workflow_sha" != "$approved_sha" ] || [ "$caller_sha" != "$approved_sha" ]; then
		echo "::error::tag-push caller SHA/workflow SHA must equal approved SHA $approved_sha (caller=$caller_sha workflow=$caller_workflow_sha)" >&2
		exit 1
	fi
	;;
workflow_dispatch)
	expected_ref="refs/heads/main-v2"
	if [ "$caller_workflow_sha" != "$caller_sha" ]; then
		echo "::error::recovery caller workflow SHA is $caller_workflow_sha, expected protected main-v2 SHA $caller_sha" >&2
		exit 1
	fi
	;;
*)
	echo "::error::unsupported stable release caller event: $caller_event" >&2
	exit 1
	;;
esac
if [ "$caller_ref" != "$expected_ref" ]; then
	echo "::error::orchestrated release caller ref is $caller_ref, expected $expected_ref" >&2
	exit 1
fi

echo "release authorization verified: caller=$actual_caller sha=$approved_sha"
