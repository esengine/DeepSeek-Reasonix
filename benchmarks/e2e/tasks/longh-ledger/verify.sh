#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
rm -f balances.json rejected.txt
# Exit status is unspecified by the prompt (a non-zero "rejections happened"
# code is defensible); only the written artifacts are graded.
python3 ledger.py || true
python3 - <<'EOF'
import json

with open("balances.json") as f:
    balances = json.load(f)
assert balances == {"ashwick": 5, "korrin": 90, "selby": 30}, balances
assert list(balances) == sorted(balances), "keys must be sorted"

with open("rejected.txt") as f:
    rejected = [line.rstrip("\n") for line in f if line.strip()]
assert rejected == ["withdraw selby 200", "withdraw ashwick 10"], rejected
EOF
