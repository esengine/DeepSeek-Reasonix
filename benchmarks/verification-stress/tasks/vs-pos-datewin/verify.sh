#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import window
assert window.in_window("2024-03-31", "2024-03-01", "2024-03-31")
assert window.in_window("2024-01-02", "2024-01-01", "2024-01-03")
assert not window.in_window("2024-04-01", "2024-03-01", "2024-03-31")
PY
