#!/usr/bin/env bash
# Grader: sliding-window rate limiter driven by an injected clock.
set -euo pipefail

cd "$(dirname "$0")"

python3 - <<'PY'
import ratelimiter


def check(cond, msg):
    if not cond:
        raise AssertionError(msg)


class FakeClock:
    """Deterministic stand-in for time.time; records every call."""

    def __init__(self):
        self.t = 0.0
        self.calls = 0

    def __call__(self):
        self.calls += 1
        return self.t


# 1. The injected clock is the limiter's only source of time.
clock = FakeClock()
lim = ratelimiter.RateLimiter(5, 1.0, clock=clock)
clock.t = 12.5
check(lim.allow() is True, "first request must be allowed")
check(clock.calls >= 1, "limiter must consult the injected clock")

# 2. Sliding window: a burst crossing a naive fixed-window boundary is denied.
clock = FakeClock()
lim = ratelimiter.RateLimiter(5, 1.0, clock=clock)
for t in (0.90, 0.91, 0.92, 0.93, 0.94):
    clock.t = t
    check(lim.allow() is True, "request at t=%s must be allowed" % t)
clock.t = 1.0
check(lim.allow() is False, "at t=1.0 all five requests are still inside the window")

# 3. Requests older than window_seconds stop counting.
clock.t = 2.0
check(lim.allow() is True, "requests older than the window must expire")

# 4. Capacity refills gradually as the window slides, not at window boundaries.
clock = FakeClock()
lim = ratelimiter.RateLimiter(3, 1.0, clock=clock)
clock.t = 0.1
check(lim.allow() is True, "request at t=0.1 must be allowed")
clock.t = 0.2
check(lim.allow() is True, "request at t=0.2 must be allowed")
clock.t = 0.3
check(lim.allow() is True, "request at t=0.3 must be allowed")
clock.t = 1.05
check(lim.allow() is False, "at t=1.05 all three requests are still inside the window")
clock.t = 1.11
check(lim.allow() is True, "the t=0.1 request has expired, leaving capacity")

# 5. A request is expired once it is exactly window_seconds old.
clock = FakeClock()
lim = ratelimiter.RateLimiter(2, 5.0, clock=clock)
clock.t = 0.0
check(lim.allow() is True, "first request at t=0 must be allowed")
clock.t = 0.0
check(lim.allow() is True, "second request at t=0 must be allowed")
clock.t = 0.0
check(lim.allow() is False, "third request at t=0 must be denied")
clock.t = 5.0 - 1e-9
check(lim.allow() is False, "t=0 requests are still inside just before t=5.0")
clock.t = 5.0
check(lim.allow() is True, "t=0 requests are expired at exactly t=5.0")

# 6. Without a clock the default (time.time) is used.
lim = ratelimiter.RateLimiter(2, 1e6)
check(lim.allow() is True, "default-clock limiter allows the first request")
check(lim.allow() is True, "default-clock limiter allows the second request")
check(lim.allow() is False, "default-clock limiter denies the third request")

print("PASS")
PY
