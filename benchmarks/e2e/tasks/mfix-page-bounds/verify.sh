#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
from slicer import page_items

items = list(range(10))
assert page_items(items, 0, 3) == [0, 1, 2], page_items(items, 0, 3)
assert page_items(items, 1, 3) == [3, 4, 5]
assert page_items(items, 3, 3) == [9]
assert page_items(items, 4, 3) == []
PY
