BASE_ATTEMPTS = 3


def budget(attempts, backoff_sec):
    """Total worst-case seconds spent retrying."""
    return attempts * backoff_sec
