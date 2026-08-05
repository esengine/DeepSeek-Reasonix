#!/usr/bin/env bash
# Report-only complexity monitor for internal/agent production code.
# Never fails the job (issues-exit-code=0). Used to track funlen/cyclop
# trends after the Run/executeOne extraction without duplicating the main
# golangci-lint quality gate.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

CONFIG="${AGENT_COMPLEXITY_CONFIG:-.golangci-agent-complexity.yml}"
OUT_DIR="${AGENT_COMPLEXITY_OUT:-.}"
REPORT="${OUT_DIR}/agent-complexity-report.txt"
SUMMARY_FILE="${GITHUB_STEP_SUMMARY:-}"

mkdir -p "$(dirname "$REPORT")"

echo "Agent complexity report (report-only, non-blocking)"
echo "config=${CONFIG}"
echo "scope=./internal/agent (tests excluded)"
echo

set +e
# Match the pinned CI golangci-lint version when available; fall back to PATH.
if command -v golangci-lint >/dev/null 2>&1; then
  golangci-lint run \
    --config "$CONFIG" \
    --issues-exit-code=0 \
    --timeout=5m \
    ./internal/agent/... >"$REPORT" 2>&1
  status=$?
else
  echo "golangci-lint not found on PATH; skipping complexity report" | tee "$REPORT"
  status=0
fi
set -e

# Always surface the report.
cat "$REPORT"
echo

# Summarize counts for humans and CI step summaries.
cyclop_count=$(grep -c '(cyclop)$' "$REPORT" 2>/dev/null || true)
funlen_count=$(grep -c '(funlen)$' "$REPORT" 2>/dev/null || true)
# grep -c exits 1 on zero matches under set -e; coerce empty to 0.
cyclop_count=${cyclop_count:-0}
funlen_count=${funlen_count:-0}
total=$((cyclop_count + funlen_count))

# Extract the highest reported cyclomatic complexity if present.
max_complexity=$(grep -oE 'complexity for function [^ ]+ is [0-9]+' "$REPORT" 2>/dev/null \
  | grep -oE '[0-9]+$' \
  | sort -nr \
  | head -1 || true)
max_complexity=${max_complexity:-0}

# Extract the longest statement count if present.
max_statements=$(grep -oE 'too many statements \([0-9]+' "$REPORT" 2>/dev/null \
  | grep -oE '[0-9]+' \
  | sort -nr \
  | head -1 || true)
max_statements=${max_statements:-0}

summary=$(cat <<EOF
### Agent complexity monitor (report-only)

| Metric | Value |
| --- | ---: |
| cyclop findings | ${cyclop_count} |
| funlen findings | ${funlen_count} |
| total findings | ${total} |
| max cyclomatic complexity | ${max_complexity} |
| max statements (funlen) | ${max_statements} |

Scope: \`internal/agent\` production code only (tests excluded).
Mode: report-only (\`issues-exit-code=0\`). Does not fail CI.
Thresholds: funlen lines=120 / statements=80, cyclop max=30.
Future work: block only new violations relative to this baseline.
EOF
)

echo "$summary"
if [[ -n "$SUMMARY_FILE" ]]; then
  echo "$summary" >>"$SUMMARY_FILE"
fi

# Never block: surface tool failures as warnings only.
if [[ "$status" -ne 0 ]]; then
  echo "::warning::agent complexity report command exited with status ${status} (report-only; not failing CI)"
fi
exit 0
