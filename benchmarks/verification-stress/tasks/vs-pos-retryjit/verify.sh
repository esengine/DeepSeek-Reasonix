#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
from backoff import delay_ms
assert delay_ms(100, 1, 10_000) == 100
assert delay_ms(100, 3, 10_000) == 400
assert delay_ms(100, 20, 10_000) == 10_000
PY
