#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
from cart import total_cents
from receipt import for_cart, format_total

assert total_cents([1000, 234]) == 1234
assert format_total(1234) == "$12.34", format_total(1234)
assert format_total(5) == "$0.05", format_total(5)
assert for_cart([199, 250, 1]) == "$4.50", for_cart([199, 250, 1])
PY
