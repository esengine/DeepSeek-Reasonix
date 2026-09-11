#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import logging
import orders, shipping
records = []
class Cap(logging.Handler):
    def emit(self, r): records.append(r)
for name in ("orders", "shipping"):
    lg = logging.getLogger(name); lg.setLevel(logging.INFO); lg.addHandler(Cap())
orders.place({"id": "o1"}, request_id="r-9")
shipping.dispatch({"id": "o1"}, request_id="r-9")
assert len(records) >= 2
for r in records:
    assert getattr(r, "request_id", None) == "r-9", r.name
PY
