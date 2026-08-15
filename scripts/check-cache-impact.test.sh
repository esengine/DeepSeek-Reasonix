#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
check="$repo_root/scripts/check-cache-impact.sh"

reviewed_body=$'Cache-impact: low - standing instructions change the stable prefix once\nCache-guard: bash scripts/check-cache-impact.test.sh\nSystem-prompt-review: reviewed as a durable static instruction change'

PR_BODY="$reviewed_body" "$check" REASONIX.md >/dev/null
PR_BODY="$reviewed_body" "$check" services/api/AGENTS.local.md >/dev/null

PR_BODY=$'Cache-impact: none - refactor preserves provider-visible bytes\nCache-guard: go test ./internal/boot\nSystem-prompt-review: reviewed provider-visible output as unchanged' \
	"$check" internal/boot/boot.go >/dev/null

PR_BODY='' "$check" docs/GUIDE.md >/dev/null

if PR_BODY='' "$check" CLAUDE.md >/dev/null 2>&1; then
	echo "standing instruction without cache declarations unexpectedly passed" >&2
	exit 1
fi

if PR_BODY=$'Cache-impact: none - text-only policy update\nCache-guard: bash scripts/check-cache-impact.test.sh\nSystem-prompt-review: reviewed as static' \
	"$check" REASONIX.local.md >/dev/null 2>&1; then
	echo "standing instruction declared as no cache impact unexpectedly passed" >&2
	exit 1
fi

if PR_BODY=$'Cache-impact: low - standing prefix changes once\nCache-guard: bash scripts/check-cache-impact.test.sh\nSystem-prompt-review: N/A' \
	"$check" nested/AGENTS.md >/dev/null 2>&1; then
	echo "standing instruction without an explicit system-prompt review unexpectedly passed" >&2
	exit 1
fi

echo "cache impact contract tests: PASS"
