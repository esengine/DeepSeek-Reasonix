#!/usr/bin/env bash
# Grader for the interval-merge task.
# Runs from a directory that holds interval_merge.py at the top level.
set -e

cd "$(dirname "$0")"

python3 - <<'PY'
import sys

sys.path.insert(0, ".")

from interval_merge import merge_intervals


def norm(intervals):
    return [tuple(iv) for iv in intervals]


CASES = [
    # Basic overlap plus disjoint intervals.
    ([(1, 3), (2, 6), (8, 10), (15, 18)], [(1, 6), (8, 10), (15, 18)]),
    # Adjacent half-open intervals do not overlap and must stay separate.
    ([(0, 1), (1, 2)], [(0, 1), (1, 2)]),
    # Fully contained interval.
    ([(1, 10), (2, 5)], [(1, 10)]),
    # Chain of overlaps collapses to one interval.
    ([(0, 2), (1, 3), (2, 4)], [(0, 4)]),
    # Several adjacent intervals, none overlapping.
    ([(0, 1), (1, 2), (2, 3)], [(0, 1), (1, 2), (2, 3)]),
    # Unsorted input is handled.
    ([(5, 6), (1, 2), (3, 4)], [(1, 2), (3, 4), (5, 6)]),
    # Empty input.
    ([], []),
    # Single interval.
    ([(2, 3)], [(2, 3)]),
    # Overlap sharing the same end.
    ([(0, 5), (3, 5)], [(0, 5)]),
]

for index, (inp, expected) in enumerate(CASES, start=1):
    got = merge_intervals(inp)
    if norm(got) != norm(expected):
        raise AssertionError(
            "case %d: merge_intervals(%r) returned %r; expected %r"
            % (index, inp, got, expected)
        )

print("OK: all %d cases passed" % len(CASES))
PY
