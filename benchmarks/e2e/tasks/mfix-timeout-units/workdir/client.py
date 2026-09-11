from api import timeout_ms


def wait_seconds():
    """How long one request may wait, in seconds."""
    return timeout_ms()


def retry_budget_seconds(attempts=3):
    return attempts * wait_seconds()
