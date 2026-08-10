#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
from pricing import discounted

assert discounted(1000, 25) == 750, discounted(1000, 25)
assert discounted(999, 10) == 900, discounted(999, 10)
assert discounted(500, 0) == 500
assert discounted(100, 100) == 0
PY
