def wire_fee(amount):
    """Return the wire fee for `amount`: the greater of 15.0 and 0.1% of it.

    A zero or negative amount has no wire fee, so it returns 0.0.
    """
    if amount <= 0:
        return 0.0
    return max(15.0, amount * 0.001)
