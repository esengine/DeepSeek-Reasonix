#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import inspect

import schedule

assert schedule.retry_times(100.0, 4) == [100.5, 101.5, 103.5, 107.5], schedule.retry_times(100.0, 4)
assert schedule.retry_times(100.0, 6) == [100.5, 101.5, 103.5, 107.5, 115.5, 123.5]
assert schedule.retry_times(0.0, 0) == []
assert "next_delay" in inspect.getsource(schedule), "schedule.py must use backoff.next_delay"
PY
