def page_bounds(page, size):
    """(start, end) bounds of a zero-based page."""
    start = page * size
    return start, start + size - 1
