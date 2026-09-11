def latest(versions):
    """Return the newest semantic version string, e.g. '1.10.0'."""
    if not versions:
        raise ValueError("no versions")
    return max(versions)


def is_upgrade(current, candidate):
    return latest([current, candidate]) == candidate and current != candidate
