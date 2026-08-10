def humanise(n):
    """Compact number: below 1000 the plain integer, otherwise one decimal with
    the unit, rounded half up. 1250 -> '1.3k', 1234 -> '1.2k',
    3_450_000 -> '3.5M'.
    """
    if n < 1000:
        return str(n)
    if n < 1000000:
        return "%d.%dk" % (n // 1000, (n % 1000) // 100)
    return "%d.%dM" % (n // 1000000, (n % 1000000) // 100000)
