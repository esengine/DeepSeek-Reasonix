#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
from ids import make_id
from index import numeric_key

assert make_id(7) == "u-7", make_id(7)
assert numeric_key(make_id(7)) == 7
assert numeric_key(make_id(120)) == 120
assert sorted([make_id(11), make_id(2)], key=numeric_key) == ["u-2", "u-11"]
PY
