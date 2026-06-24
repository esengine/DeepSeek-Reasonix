#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage: scripts/check-cache-impact.sh [changed-file ...]

Checks that PRs touching cache-sensitive prompt or tool surfaces include an
explicit cache-impact note and guard-test note in the pull request body.

Inputs:
  CACHE_IMPACT_PR_BODY or PR_BODY          Pull request body text.
  CACHE_IMPACT_PR_BODY_FILE               File containing the pull request body.
  CACHE_IMPACT_CHANGED_FILES              Newline-separated changed files.
  CACHE_IMPACT_CHANGED_FILES_FILE         File containing newline-separated changed files.
  CACHE_IMPACT_BASE_SHA / BASE_SHA        Diff base when files are not supplied.
  CACHE_IMPACT_HEAD_SHA / HEAD_SHA        Diff head when files are not supplied.
USAGE
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

body="${CACHE_IMPACT_PR_BODY:-${PR_BODY:-}}"
if [[ -n "${CACHE_IMPACT_PR_BODY_FILE:-}" ]]; then
  body="$(cat "$CACHE_IMPACT_PR_BODY_FILE")"
fi

changed_input=""
if [[ "$#" -gt 0 ]]; then
  changed_input="$(printf '%s\n' "$@")"
elif [[ -n "${CACHE_IMPACT_CHANGED_FILES_FILE:-}" ]]; then
  changed_input="$(cat "$CACHE_IMPACT_CHANGED_FILES_FILE")"
elif [[ -n "${CACHE_IMPACT_CHANGED_FILES:-}" ]]; then
  changed_input="$CACHE_IMPACT_CHANGED_FILES"
else
  base="${CACHE_IMPACT_BASE_SHA:-${BASE_SHA:-}}"
  head="${CACHE_IMPACT_HEAD_SHA:-${HEAD_SHA:-HEAD}}"
  if [[ -z "$base" ]]; then
    base="$(git merge-base origin/main-v2 "$head" 2>/dev/null || git merge-base main-v2 "$head")"
  fi
  diff_base="$base"
  if merge_base="$(git merge-base "$base" "$head" 2>/dev/null)"; then
    diff_base="$merge_base"
  fi
  changed_input="$(git diff --name-only "$diff_base" "$head")"
fi

changed_files=()
while IFS= read -r file; do
  [[ -z "$file" ]] && continue
  changed_files+=("$file")
done <<< "$changed_input"

cache_sensitive=()
system_prompt_sensitive=()

for file in "${changed_files[@]:-}"; do
  case "$file" in
    internal/agent/agent.go|\
    internal/agent/ask.go|\
    internal/agent/cache*|\
    internal/agent/compact*|\
    internal/agent/parallel_tasks.go|\
    internal/agent/prune*|\
    internal/agent/subagent_registry*|\
    internal/agent/task.go|\
    internal/boot/*|\
    internal/command/slashtool.go|\
    internal/config/config.go|\
    internal/config/system_prompt*|\
    internal/history/tool.go|\
    internal/installsource/*|\
    internal/lsp/tool.go|\
    internal/memory/*|\
    internal/outputstyle/*|\
    internal/plugin/*|\
    internal/provider/*|\
    internal/skill/*|\
    internal/tool/*|\
    scripts/cache-guard.sh|\
    scripts/check-cache-impact.sh)
      cache_sensitive+=("$file")
      ;;
  esac

  case "$file" in
    internal/agent/task.go|\
    internal/boot/*|\
    internal/config/config.go|\
    internal/config/system_prompt*|\
    internal/memory/*|\
    internal/outputstyle/*|\
    internal/skill/*)
      system_prompt_sensitive+=("$file")
      ;;
  esac
done

if [[ "${#cache_sensitive[@]}" -eq 0 ]]; then
  echo "No cache-sensitive prompt/tool files changed."
  exit 0
fi

failures=()

trim() {
  local s="$1"
  s="${s#"${s%%[![:space:]]*}"}"
  s="${s%"${s##*[![:space:]]}"}"
  printf '%s' "$s"
}

field_value() {
  local label="$1"
  local line
  line="$(printf '%s\n' "$body" | grep -Eim1 "^[[:space:]>#*_-]*${label}[[:space:]]*:" || true)"
  [[ -z "$line" ]] && return 1
  trim "${line#*:}"
}

require_field() {
  local label="$1"
  local value
  if ! value="$(field_value "$label")"; then
    failures+=("missing ${label}: line")
    return
  fi
  if [[ -z "$value" || "$value" =~ ^[Tt][Oo][Dd][Oo]($|[[:space:]:-]) || "$value" =~ ^[Tt][Bb][Dd]($|[[:space:]:-]) ]]; then
    failures+=("${label}: must be filled out")
  fi
}

require_review_field() {
  local label="$1"
  local value
  if ! value="$(field_value "$label")"; then
    failures+=("missing ${label}: line")
    return
  fi
  local lower
  lower="$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')"
  if [[ -z "$value" || "$lower" =~ ^todo($|[[:space:]:-]) || "$lower" =~ ^tbd($|[[:space:]:-]) || "$lower" =~ ^n/?a($|[[:space:]:-]) || "$lower" =~ ^none($|[[:space:]:-]) ]]; then
    failures+=("${label}: must name the explicit system-prompt review/approval")
  fi
}

require_field "Cache-impact"
require_field "Cache-guard"

if [[ "${#system_prompt_sensitive[@]}" -gt 0 ]]; then
  require_review_field "System-prompt-review"
fi

if [[ "${#failures[@]}" -gt 0 ]]; then
  {
    echo "Cache impact check failed."
    echo
    echo "Cache-sensitive files changed:"
    printf '  - %s\n' "${cache_sensitive[@]}"
    if [[ "${#system_prompt_sensitive[@]}" -gt 0 ]]; then
      echo
      echo "System-prompt-sensitive files changed:"
      printf '  - %s\n' "${system_prompt_sensitive[@]}"
    fi
    echo
    echo "Required PR body lines:"
    echo "  Cache-impact: <none|low|medium|high> - <reason>"
    echo "  Cache-guard: <focused guard test/command or existing guard rationale>"
    if [[ "${#system_prompt_sensitive[@]}" -gt 0 ]]; then
      echo "  System-prompt-review: <reviewer/approval note>"
    fi
    echo
    echo "Failures:"
    printf '  - %s\n' "${failures[@]}"
  } >&2
  exit 1
fi

echo "Cache impact check passed."
echo "Cache-sensitive files:"
printf '  - %s\n' "${cache_sensitive[@]}"
