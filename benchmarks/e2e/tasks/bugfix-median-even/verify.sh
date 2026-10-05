#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
from stats import median

assert median([1, 3]) == 2, median([1, 3])
assert median([1, 2, 3, 4]) == 2.5, median([1, 2, 3, 4])
assert median([5]) == 5
assert median([4, 1, 3, 2]) == 2.5
assert median([1, 2, 3]) == 2
PY
