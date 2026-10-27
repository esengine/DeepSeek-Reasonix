#!/usr/bin/env bash
# Reads changed file names (NUL-separated, as `git diff -z --name-only --no-renames`
# prints them) on stdin and prints "true" when a
# macOS/Windows test leg is worth running for them, "false" otherwise.
# Run from the repository root. Unknown paths count as platform-relevant: the
# set below names what is provably platform-neutral, never the reverse.
set -euo pipefail

neutral_dir='^(docs/|release-notes/|workers/|benchmarks/|npm/|examples/|desktop/frontend-next/)'
# Kernel packages whose behaviour is OS-specific (files, processes, shells,
# sandboxes, sockets, paths) even where no file carries a build tag.
platform_dir='^(internal/(safety|platform|base|state|ext|tools|session|assembly)/|internal/runtime/(isolation|delegation)/|cmd/|sdk/|tools/|scripts/|desktop/(electron|cmd|packaging|computer-helper|internal)/|\.github/)'
platform_file='(^|/)(go\.(mod|sum)|Makefile)$|\.(nsh|nsi|ps1|sh|bat|cmd)$|_(windows|darwin|linux|unix|bsd)(_test)?\.go$'

seen=0
while IFS= read -r -d '' f; do
  [ -n "$f" ] || continue
  seen=1
  case "$f" in
    *.md) [[ "$f" == */* && ! "$f" =~ $neutral_dir ]] && { echo true; exit 0; } ; continue ;;
  esac
  if [[ "$f" =~ $neutral_dir ]]; then continue; fi
  if [[ "$f" =~ $platform_dir || "$f" =~ $platform_file ]]; then echo true; exit 0; fi
  if [[ "$f" == *.go ]]; then
    d=$(dirname -- "$f")
    # A package that carries any OS-constrained file can break on the OS it
    # does not build for, whichever file the change touched.
    if [ -d "$d" ] && grep -lE '^//go:build .*\b(windows|darwin|linux|unix|freebsd|openbsd|netbsd|ios|android|cgo)\b' -- "$d"/*.go >/dev/null 2>&1; then echo true; exit 0; fi
    continue
  fi
  echo true; exit 0
done
[ "$seen" = 1 ] && echo false || echo true
