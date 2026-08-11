def fmt_amount(cents, currency):
    """Format cents as a currency string, e.g. 1234 -> '12.34 USD'."""
    return "%d.%02d %s" % (cents // 100, cents % 100, currency)
