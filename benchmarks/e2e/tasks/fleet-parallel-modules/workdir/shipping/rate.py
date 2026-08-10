def rate_cents(weight_g):
    """Flat 500 up to and including 1000g, 1000 above that."""
    if weight_g < 1000:
        return 500
    return 1000
