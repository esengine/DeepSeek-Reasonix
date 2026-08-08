#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
with open("contacts_clean.txt") as f:
    lines = [line.strip() for line in f if line.strip()]
lowered = [line.lower() for line in lines]
assert len(lowered) == len(set(lowered)), "duplicates survived the cleanup"
assert set(lowered) == {
    "ada@example.com",
    "bo@example.com",
    "cy@example.com",
    "dee@example.com",
}, sorted(lowered)
PY
