from limits.budget import RETRY_BUDGET


def dispatch(send):
    """Retry a failed send up to the budget the limits package defines."""
    err = None
    for _ in range(RETRY_BUDGET):
        err = send()
        if err is None:
            return None
    return err
