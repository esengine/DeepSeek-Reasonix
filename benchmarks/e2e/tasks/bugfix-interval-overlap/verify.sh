#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
from intervals import overlaps

assert overlaps((0, 2), (1, 3))
assert overlaps((1, 3), (0, 2))
assert not overlaps((0, 1), (1, 2)), "touching half-open intervals do not overlap"
assert overlaps((0, 5), (2, 3))
assert not overlaps((3, 4), (0, 2))
PY
