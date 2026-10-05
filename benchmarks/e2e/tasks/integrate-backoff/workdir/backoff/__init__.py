def next_delay(attempt):
    """See README.md: min(8.0, 0.5 * 2**attempt), attempt 0-based."""
    if attempt < 0:
        raise ValueError("attempt must be >= 0")
    return min(8.0, 0.5 * 2**attempt)
