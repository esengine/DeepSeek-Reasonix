#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
from metrics import p95, rate
assert p95([1, 2, 3, 4, 5]) == 5
assert p95([]) == 0
assert rate([], 0) == 0.0
assert abs(rate([1, 2], 4) - 0.5) < 1e-9
PY
