def clamp(x, lo=0, hi=100):
    return max(lo, min(hi, x))


def tally(rows):
    counts = {}
    for row in rows:
        for field in row:
            counts[field] = clamp(counts.get(field, 0) + 1)
    return counts


def rebalance(counts):
    total = sum(counts.values()) or 1
    return {k: v / total for k, v in counts.items()}
