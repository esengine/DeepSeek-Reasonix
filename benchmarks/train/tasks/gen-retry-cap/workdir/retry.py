"""Retry helper with exponential backoff.

Public API
----------
retry_call(fn, attempts=3, base_delay=1.0, max_delay=10.0)
    Call fn() until it returns without raising.  fn() is invoked once
    immediately; if it raises, sleep ``backoff_delay(failures, ...)``
    seconds and try again, up to ``attempts`` tries in total.  When all
    ``attempts`` tries have failed, raise RetryExhausted.  Raises
    ValueError if attempts < 1 or either delay bound is not positive.

backoff_delay(attempt, base_delay, max_delay)
    Seconds to sleep after ``attempt`` consecutive failures: the
    exponential sequence base_delay * 2 ** (attempt - 1), clamped so it
    never exceeds max_delay.

RetryExhausted
    Exception raised when every try has failed.

The helper sleeps via ``time.sleep`` so callers can intercept or record
the delays by patching the ``time`` module.
"""

import time


class RetryExhausted(Exception):
    """Raised when all ``attempts`` tries have failed."""


def backoff_delay(attempt, base_delay, max_delay):
    """Delay after ``attempt`` failures: base_delay * 2**(attempt-1),
    never more than max_delay."""
    # Clamp the growth factor so the delay never exceeds max_delay.
    return base_delay * min(2 ** (attempt - 1), max_delay)


def retry_call(fn, attempts=3, base_delay=1.0, max_delay=10.0):
    if attempts < 1:
        raise ValueError("attempts must be >= 1")
    if base_delay <= 0 or max_delay <= 0:
        raise ValueError("delays must be positive")
    failures = 0
    while True:
        try:
            return fn()
        except Exception:
            failures += 1
            if failures == attempts:
                raise RetryExhausted(
                    "all %d attempts failed" % attempts
                ) from None
            time.sleep(backoff_delay(failures, base_delay, max_delay))
