"""Compare two semantic version strings.

`compare(a, b)` returns -1 if `a` sorts before `b`, 0 if they are equal,
and 1 if `a` sorts after `b`.
"""


def compare(a, b):
    """Compare semantic version strings ``a`` and ``b``."""

    def split(v):
        core, _, pre = v.partition("-")
        return [int(p) for p in core.split(".")], pre

    (an, ap), (bn, bp) = split(a), split(b)

    if an != bn:
        return -1 if an < bn else 1

    if ap == bp:
        return 0
    if not ap:
        return 1
    if not bp:
        return -1

    # Compare pre-release strings as plain text.
    return -1 if ap < bp else 1
