#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import index
idx = index.build({1: "red fox.", 2: "Red hen"})
assert index.lookup(idx, "red") == [1, 2], index.lookup(idx, "red")
idx = index.build({7: "end."})
assert index.lookup(idx, "end") == [7]
PY
