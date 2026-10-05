"""Sliding-window rate limiter driven by an injected clock."""

import time
from collections import deque


class RateLimiter:
    """Rate-limits requests with a sliding time window.

    A request is allowed when fewer than `max_requests` requests were recorded
    at times strictly more recent than `now - window_seconds`, where `now`
    comes from the injected clock (default: `time.time`). Allowed requests are
    recorded; denied requests are not.
    """

    def __init__(self, max_requests, window_seconds, clock=None):
        self.max_requests = max_requests
        self.window_seconds = window_seconds
        self.clock = clock if clock is not None else time.time
        self._timestamps = deque()

    def allow(self):
        now = self.clock()
        cutoff = now - self.window_seconds
        while self._timestamps and self._timestamps[0] <= cutoff:
            self._timestamps.popleft()
        if len(self._timestamps) >= self.max_requests:
            return False
        self._timestamps.append(now)
        return True
