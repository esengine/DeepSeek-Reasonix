class TokenBucket:
    """See README.md; the bucket starts full at the first allow() timestamp."""

    def __init__(self, capacity, refill_per_sec):
        if capacity <= 0 or refill_per_sec <= 0:
            raise ValueError("capacity and refill_per_sec must be positive")
        self.capacity = float(capacity)
        self.refill_per_sec = float(refill_per_sec)
        self._tokens = self.capacity
        self._last_t = None

    def allow(self, t):
        if self._last_t is not None:
            if t < self._last_t:
                raise ValueError("timestamps must be monotonically non-decreasing")
            self._tokens = min(
                self.capacity, self._tokens + (t - self._last_t) * self.refill_per_sec
            )
        self._last_t = t
        if self._tokens >= 1:
            self._tokens -= 1
            return True
        return False
