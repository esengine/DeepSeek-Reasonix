#!/usr/bin/env bash
set -euo pipefail

prefix="${1:-}"
run_id="${2:-}"
sealed_attempt="${3:-}"

if ! [[ "$run_id" =~ ^[1-9][0-9]*$ && "$sealed_attempt" =~ ^[1-9][0-9]*$ ]]; then
  echo "invalid candidate producer identity" >&2
  exit 1
fi
if ! [[ "$prefix" =~ ^desktop-${run_id}-([1-9][0-9]*)-preflight$ ]]; then
  echo "Desktop artifact prefix belongs to another producer" >&2
  exit 1
fi
if ! test "${BASH_REMATCH[1]}" -le "$sealed_attempt"; then
  echo "Desktop artifacts were built after the sealing attempt" >&2
  exit 1
fi
