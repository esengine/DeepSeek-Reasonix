def overlaps(a, b):
    """Whether half-open intervals [a0, a1) and [b0, b1) intersect."""
    return a[0] < b[1] and b[0] < a[0]
