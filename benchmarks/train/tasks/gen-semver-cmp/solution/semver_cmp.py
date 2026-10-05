"""Compare two semantic version strings.

`compare(a, b)` returns -1 if `a` sorts before `b`, 0 if they are equal,
and 1 if `a` sorts after `b`, following the SemVer 2.0.0 precedence rules.
"""


def compare(a, b):
    """Compare semantic version strings ``a`` and ``b``."""

    def parse(v):
        core, _, pre = v.partition("-")
        nums = [int(p) for p in core.split(".")]
        prerelease = pre.split(".") if pre else None
        return nums, prerelease

    (an, ap), (bn, bp) = parse(a), parse(b)

    if an != bn:
        return -1 if an < bn else 1

    if ap is None and bp is None:
        return 0
    if ap is None:
        return 1
    if bp is None:
        return -1

    for x, y in zip(ap, bp):
        x_num = x.isdigit()
        y_num = y.isdigit()
        if x_num and y_num:
            if int(x) != int(y):
                return -1 if int(x) < int(y) else 1
        elif x_num:
            # Numeric identifiers have lower precedence than alphanumeric.
            return -1
        elif y_num:
            return 1
        elif x != y:
            return -1 if x < y else 1

    # All shared identifiers are equal: more fields wins.
    if len(ap) != len(bp):
        return -1 if len(ap) < len(bp) else 1
    return 0
