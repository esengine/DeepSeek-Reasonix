#!/usr/bin/env bash
#
# Grader for the retry-backoff task.
#
# Runs from a directory where the workdir/ contents sit at the top level
# (retry.py beside this script).  Grades public behaviour only: backoff
# delays must grow exponentially with each consecutive failure and must
# never exceed max_delay, and retry_call must actually sleep those
# delays.  All checks are independent of the test file shipped with the
# task, so weakening or deleting tests cannot fool the grader.
#
#   bash verify.sh    # exit 0 on a correct fix, non-zero otherwise
#
set -e

cd "$(dirname "$0")"

python3 - <<'PY'
import importlib.util
import sys
import types
import time as _real_time


def load_retry():
    """Import ./retry.py with a fake time module that records every sleep.

    Intercepting the module at load time (via sys.modules) means the
    recording works whether retry.py uses ``import time`` or
    ``from time import sleep``.
    """
    recorded = []
    fake = types.ModuleType("time")
    fake.sleep = lambda d: recorded.append(d)
    for name in dir(_real_time):
        if not hasattr(fake, name):
            setattr(fake, name, getattr(_real_time, name))

    old = sys.modules.get("time")
    sys.modules["time"] = fake
    try:
        spec = importlib.util.spec_from_file_location("retry", "retry.py")
        mod = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(mod)
    finally:
        if old is not None:
            sys.modules["time"] = old
        else:
            del sys.modules["time"]
    return mod, recorded


# --- backoff_delay: exponential growth, clamped at max_delay -----------
mod, _ = load_retry()
seq = [mod.backoff_delay(n, 2.0, 5.0) for n in range(1, 9)]
assert seq == [2.0, 4.0, 5.0, 5.0, 5.0, 5.0, 5.0, 5.0], seq

# The cap holds even when base_delay alone exceeds max_delay.
assert mod.backoff_delay(1, 7.0, 5.0) == 5.0
assert mod.backoff_delay(2, 7.0, 5.0) == 5.0

# A differently-scaled case: never exceed the cap for many attempts.
for n in range(1, 20):
    d = mod.backoff_delay(n, 1.5, 3.0)
    assert d <= 3.0, (n, d)
assert mod.backoff_delay(5, 1.5, 3.0) == 3.0

# --- retry_call: must actually sleep the capped sequence ---------------
mod, recorded = load_retry()
calls = []

def flaky():
    calls.append(1)
    if len(calls) < 4:
        raise ValueError("boom")
    return "ok"

assert mod.retry_call(flaky, attempts=4, base_delay=2.0, max_delay=5.0) == "ok"
assert len(calls) == 4
assert recorded == [2.0, 4.0, 5.0], recorded

# --- exhaustion, first-try success, argument validation ----------------
mod, recorded = load_retry()

def always_fail():
    raise RuntimeError("nope")

try:
    mod.retry_call(always_fail, attempts=3, base_delay=0.5, max_delay=2.0)
    raise SystemExit("retry_call should have raised RetryExhausted")
except mod.RetryExhausted:
    pass
assert len(recorded) == 2

mod, recorded = load_retry()
assert mod.retry_call(lambda: "ok", attempts=3) == "ok"
assert recorded == []

mod, _ = load_retry()
for bad in (dict(attempts=0), dict(base_delay=0.0), dict(max_delay=-1.0)):
    try:
        mod.retry_call(lambda: 1, **bad)
        raise SystemExit("expected ValueError for %r" % (bad,))
    except ValueError:
        pass

print("verify.sh: PASS")
PY
