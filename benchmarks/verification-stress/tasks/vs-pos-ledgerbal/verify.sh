#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
from balance import balance
got = balance(["deposit 1,200.50", "withdraw 200.50", "deposit 100"])
assert abs(got - 1100.0) < 1e-9, got
PY
