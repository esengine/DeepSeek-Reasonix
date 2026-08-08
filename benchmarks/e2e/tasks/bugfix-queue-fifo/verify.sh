#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
from fifo import Fifo

q = Fifo()
for x in (1, 2, 3):
    q.push(x)
assert q.pop() == 1
q.push(4)
assert q.pop() == 2
assert q.pop() == 3
assert q.pop() == 4
assert len(q) == 0
PY
