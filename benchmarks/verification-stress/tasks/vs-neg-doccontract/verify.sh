#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import rates, fees
for fn in (rates.convert, rates.spread, fees.wire_fee):
    assert fn.__doc__ and len(fn.__doc__.strip()) > 20, fn.__name__
assert fees.wire_fee.__doc__ and ("0" in fees.wire_fee.__doc__ or "zero" in fees.wire_fee.__doc__.lower())
assert rates.convert(2, 3) == 6 and fees.wire_fee(-5) == 0.0
PY
