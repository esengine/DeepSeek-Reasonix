#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
from pricing import final_price
for args, want in [((100.0, 10, 5.0), 85.0), ((10.0, 0, 15.0), 0.0), ((20.0, 25, 2.5), 12.5)]:
    got = final_price(*args)
    assert abs(got - want) < 1e-9, (args, got, want)
PY
