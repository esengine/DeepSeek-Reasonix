# backoff

Exponential backoff delays.

```python
from backoff import next_delay

next_delay(attempt)  # attempt is 0-based
```

`next_delay(k)` returns `min(8.0, 0.5 * 2**k)` seconds: 0.5, 1.0, 2.0, 4.0,
then capped at 8.0 for every later attempt.
