#!/usr/bin/env bash
# Produce candidate task bundles from specs.txt. Validation is the corpus test,
# not this script: a candidate is kept only if `go test -run Corpus` accepts it.
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
repo="$(cd "$here/../.." && pwd)"
bin="${REASONIX_BIN:-reasonix}"
limit="${1:-0}"
staging="$(mktemp -d)"
trap 'rm -rf "$staging"' EXIT

read -r -d '' brief <<'BRIEF' || true
Author one benchmark task bundle in the current directory. Create exactly:

  task.toml    prompt = "<what the agent must do>", class = "<CLASS>", timeout_sec = 300
  verify.sh    bash grader, `set -e`, python3 stdlib only
  workdir/     the seed files the agent starts from
  solution/    the reference fix, files that overlay workdir/

verify.sh runs from a directory holding workdir/'s contents at the top level,
so it references `foo.py`, never `workdir/foo.py`.

Two hard requirements. Check both yourself with real commands before you finish:

  1. Copy workdir/ to a scratch directory, copy verify.sh beside it, run it.
     It MUST fail. A grader that passes on the seed measures nothing.
  2. Do it again but also copy solution/ over the top, and run it.
     It MUST pass. A grader nothing can satisfy trains a model to give up.

Grade the requirement, not the implementation: assert on public behaviour and
values, never on internal helper names, line counts, or file layout beyond what
task.toml asks for. No network, no pip, no clock or randomness the grader
cannot control. task.toml's prompt must be solvable from workdir/ alone and
must not reveal the fix.

The subject is: SUBJECT
BRIEF

n=0
while IFS='|' read -r id class topic; do
  [ -n "${id:-}" ] || continue
  case "$id" in \#*) continue ;; esac
  if [ "$limit" -gt 0 ] && [ "$n" -ge "$limit" ]; then break; fi
  n=$((n + 1))
  if [ -d "$repo/benchmarks/train/tasks/$id" ] && [ "${FORCE:-0}" != "1" ]; then
    echo "[$n] $id — already generated; FORCE=1 to redo"
    continue
  fi
  work="$staging/$id"
  mkdir -p "$work"
  prompt="${brief/SUBJECT/$topic}"
  prompt="${prompt//<CLASS>/$class}"
  echo "[$n] $id ($class)"
  ( cd "$work" && "$bin" run -y --print "$prompt" >/dev/null 2>&1 ) || {
    echo "    agent run failed; skipped"; continue; }
  if [ ! -f "$work/task.toml" ] || [ ! -f "$work/verify.sh" ]; then
    echo "    incomplete bundle; skipped"; continue
  fi
  rm -rf "${repo:?}/benchmarks/train/tasks/$id"
  mkdir -p "$repo/benchmarks/train/tasks"
  cp -R "$work" "$repo/benchmarks/train/tasks/$id"
done < "$here/specs.txt"

echo
echo "Validating with the corpus gates; failures are deleted."
cd "$repo"
go test ./cmd/e2ebench/ -run 'Corpus' -json 2>/dev/null |
  python3 "$here/prune.py"
