"""Tests for retry.py.

The helper must back off exponentially between retries but never sleep
longer than max_delay.  These tests record the actual sleeps by loading
retry.py against a fake ``time`` module, so they check what retry_call
really does, not just the delay formula.

Run with:  python3 -m unittest test_retry
"""

import importlib.util
import sys
import types
import unittest

import time as _real_time


def load_retry():
    """Import ./retry.py with time.sleep replaced by a recorder.

    Returns (module, recorded_delays).  Intercepting the module instead
    of monkeypatching after import means it works whether retry.py uses
    ``import time`` or ``from time import sleep``.
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


class BackoffDelayTests(unittest.TestCase):
    def test_grows_exponentially_until_the_cap(self):
        mod, _ = load_retry()
        delays = [mod.backoff_delay(n, 2.0, 5.0) for n in range(1, 6)]
        self.assertEqual(delays, [2.0, 4.0, 5.0, 5.0, 5.0])

    def test_cap_holds_even_when_base_delay_exceeds_it(self):
        mod, _ = load_retry()
        self.assertEqual(mod.backoff_delay(1, 7.0, 5.0), 5.0)
        self.assertEqual(mod.backoff_delay(2, 7.0, 5.0), 5.0)

    def test_never_exceeds_the_cap_for_many_attempts(self):
        mod, _ = load_retry()
        for n in range(1, 20):
            self.assertLessEqual(mod.backoff_delay(n, 1.5, 3.0), 3.0)


class RetryCallTests(unittest.TestCase):
    def test_retry_call_never_sleeps_longer_than_max_delay(self):
        mod, recorded = load_retry()
        calls = []

        def flaky():
            calls.append(1)
            if len(calls) < 4:
                raise ValueError("boom")
            return "ok"

        self.assertEqual(
            mod.retry_call(flaky, attempts=4, base_delay=2.0, max_delay=5.0),
            "ok",
        )
        self.assertEqual(len(calls), 4)
        self.assertEqual(recorded, [2.0, 4.0, 5.0])
        for d in recorded:
            self.assertLessEqual(d, 5.0)

    def test_raises_retry_exhausted_after_attempts(self):
        mod, recorded = load_retry()

        def always_fail():
            raise RuntimeError("nope")

        with self.assertRaises(mod.RetryExhausted):
            mod.retry_call(always_fail, attempts=3, base_delay=0.5, max_delay=2.0)
        self.assertEqual(len(recorded), 2)

    def test_success_on_first_try_does_not_sleep(self):
        mod, recorded = load_retry()
        self.assertEqual(mod.retry_call(lambda: "ok", attempts=3), "ok")
        self.assertEqual(recorded, [])

    def test_rejects_invalid_arguments(self):
        mod, _ = load_retry()
        with self.assertRaises(ValueError):
            mod.retry_call(lambda: 1, attempts=0)
        with self.assertRaises(ValueError):
            mod.retry_call(lambda: 1, base_delay=0.0)
        with self.assertRaises(ValueError):
            mod.retry_call(lambda: 1, max_delay=-1.0)


if __name__ == "__main__":
    unittest.main()
