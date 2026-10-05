"""Sliding-window rate limiter with an injectable clock."""

import time


class RateLimiter:
    """Limits how many requests may pass within a time window."""

    def __init__(self, max_requests, window_seconds, clock=None):
        self.max_requests = max_requests
        self.window_seconds = window_seconds
        self.clock = clock or time.time
        self._count = 0
        self._window_start = None

    def allow(self):
        now = time.time()
        if self._window_start is None:
            self._window_start = now
            self._count = 1
            return True
        if now - self._window_start >= self.window_seconds:
            self._window_start = now
            self._count = 1
            return True
        if self._count >= self.max_requests:
            return False
        self._count += 1
        return True
