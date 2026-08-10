def rank(players):
    """Players sorted by score descending; ties keep submission order."""
    return sorted(players, key=lambda p: (-p[1], p[0]))
