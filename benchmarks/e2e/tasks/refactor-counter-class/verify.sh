#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import inspect

import metrics

metrics.reset()
assert metrics.bump() == 1
assert metrics.bump(4) == 5
assert metrics.value() == 5
metrics.reset()
assert metrics.value() == 0

a, b = metrics.Counter(), metrics.Counter()
a.bump(3)
assert a.value() == 3 and b.value() == 0, "instances must not share state"

assert "global" not in inspect.getsource(metrics)
PY
