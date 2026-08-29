#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
check="$repo_root/scripts/check-docs-impact.sh"

PR_BODY='Documentation-impact: updated - documented the new command' \
	"$check" internal/cli/cli.go docs/CLI.md >/dev/null

# Nested docs (any depth under docs/) count as a docs change.
PR_BODY='Documentation-impact: updated - documented the nested design spec' \
	"$check" internal/cli/cli.go docs/superpowers/specs/2026-06-29-autoresearch-runtime-design.md >/dev/null

if PR_BODY='Documentation-impact: none - no docs changed' \
	"$check" internal/cli/cli.go docs/superpowers/specs/2026-06-29-autoresearch-runtime-design.md >/dev/null 2>&1; then
	echo "none declaration with a nested docs change unexpectedly passed" >&2
	exit 1
fi

PR_BODY='Documentation-impact: none - internal behavior keeps the existing contract' \
	"$check" internal/boot/boot.go >/dev/null

PR_BODY='' "$check" internal/agent/agent_test.go >/dev/null

if PR_BODY='' "$check" internal/cli/cli.go >/dev/null 2>&1; then
	echo "missing documentation declaration unexpectedly passed" >&2
	exit 1
fi

if PR_BODY='Documentation-impact: updated - changed behavior' \
	"$check" internal/cli/cli.go >/dev/null 2>&1; then
	echo "updated declaration without a docs change unexpectedly passed" >&2
	exit 1
fi

if PR_BODY='Documentation-impact: none' "$check" internal/cli/cli.go >/dev/null 2>&1; then
	echo "empty none rationale unexpectedly passed" >&2
	exit 1
fi

# desktop/frontend/src changes are user-visible and need a declaration.
PR_BODY='Documentation-impact: none - no user-facing copy changed' \
	"$check" desktop/frontend/src/components/editors/HljsCode.tsx >/dev/null

if PR_BODY='' "$check" desktop/frontend/src/components/editors/HljsCode.tsx >/dev/null 2>&1; then
	echo "frontend src change without a documentation declaration unexpectedly passed" >&2
	exit 1
fi

echo "documentation impact contract tests: PASS"
