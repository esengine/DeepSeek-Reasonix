#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
rm -f inventory.json rejected.txt
python3 inventory.py
python3 - <<'PY'
import json

with open("inventory.json") as f:
    inv = json.load(f)
assert inv == {
    "barrels": {"free": 0, "reserved": 0},
    "crates": {"free": 7, "reserved": 1},
}, inv
assert list(inv) == sorted(inv)

with open("rejected.txt") as f:
    rejected = [line.rstrip("\n") for line in f if line.strip()]
assert rejected == ["release crates 5", "reserve crates 7", "ship barrels 1"], rejected
PY
