#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import timings, budget, alerts
assert not hasattr(timings, "SLOW_MS") and not hasattr(timings, "BUDGET_MS")
e = timings.record(1.5)
assert abs(e["elapsed_s"] - 1.5) < 1e-9
assert alerts.is_slow(timings.record(2.5))
assert not alerts.is_slow(timings.record(1.0))
assert budget.within_budget([timings.record(30.0), timings.record(29.0)])
assert not budget.within_budget([timings.record(61.0)])
PY
