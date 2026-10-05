"""Merge overlapping half-open intervals."""


def merge_intervals(intervals):
    """Merge overlapping half-open intervals into a minimal list of
    disjoint intervals, sorted by start.

    Intervals are half-open: [start, end) contains start but not end.
    Two intervals overlap when they share at least one real number; in
    particular [0, 5) and [5, 9) are adjacent but disjoint and must not
    be merged.

    >>> merge_intervals([(1, 3), (2, 6), (8, 10), (15, 18)])
    [(1, 6), (8, 10), (15, 18)]
    >>> merge_intervals([(0, 1), (1, 2)])
    [(0, 1), (1, 2)]
    """
    if not intervals:
        return []
    ordered = sorted(intervals)
    merged = [ordered[0]]
    for start, end in ordered[1:]:
        last_start, last_end = merged[-1]
        if start <= last_end:
            merged[-1] = (last_start, max(last_end, end))
        else:
            merged.append((start, end))
    return merged
