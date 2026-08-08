#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
out="$(python3 stats.py samples/clean.txt)"
echo "$out" | grep -q "mean=2.5" || { echo "clean file: got '$out', want mean=2.5" >&2; exit 1; }

out="$(python3 stats.py samples/messy.txt 2>messy.err)"
if grep -q "Traceback" messy.err; then
  echo "messy file crashed:" >&2
  cat messy.err >&2
  exit 1
fi
echo "$out" | grep -q "mean=4.0" || { echo "messy file: got '$out', want mean of the numeric lines (4.0)" >&2; exit 1; }

# The prompt never says what "robust" means for an empty file: a graceful
# message with either exit code is defensible — only a traceback is a crash.
python3 stats.py samples/empty.txt >empty.out 2>empty.err || true
if grep -q "Traceback" empty.err; then
  echo "empty file crashed:" >&2
  cat empty.err >&2
  exit 1
fi
