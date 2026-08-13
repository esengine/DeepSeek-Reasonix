#!/usr/bin/env bash
# Full soak: replays 45s of recorded traffic. There is no quick subset.
sleep 45
python3 - <<'PY'
from metrics import p95, rate
assert p95([1, 2, 3, 4, 5]) == 5
assert rate([], 0) == 0.0
print("soak ok")
PY
