def discounted(price_cents, percent):
    """Price after an integer-percent discount, in cents, rounded down."""
    return price_cents - price_cents * (percent // 100)
