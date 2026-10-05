#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import states, transitions, report
assert set(states.TERMINAL) == {"delivered", "cancelled"}
assert "closed" not in states.STATES
assert transitions.can_move("paid", "delivered")
assert transitions.can_move("new", "cancelled")
assert not transitions.can_move("delivered", "paid")
s = report.summarize(["new", "delivered", "delivered"])
assert s["delivered"] == 2 and s["new"] == 1
PY
