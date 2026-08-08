#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import convert, tables
assert convert.c_to_f(100) == 212
assert convert.c_to_f(0) == 32
assert tables.band(35) == "hot" and tables.band(20) == "warm" and tables.band(5) == "cold"
PY
