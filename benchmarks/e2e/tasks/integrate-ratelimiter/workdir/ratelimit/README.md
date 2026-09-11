# ratelimit

Token-bucket rate limiting.

## API

```python
from ratelimit import TokenBucket

bucket = TokenBucket(capacity=3, refill_per_sec=1)
bucket.allow(timestamp)  # -> bool
```

Semantics:

- The bucket starts **full** (`capacity` tokens) at the timestamp of the first
  `allow` call.
- On every `allow(t)` call the bucket first refills:
  `tokens = min(capacity, tokens + (t - last_t) * refill_per_sec)`, then
  records `last_t = t`. The refill happens **whether or not the call is
  admitted**.
- If `tokens >= 1` after the refill, one token is consumed and `allow`
  returns `True`; otherwise it returns `False` and the (fractional) tokens
  are kept.
- Timestamps must be monotonically non-decreasing.
