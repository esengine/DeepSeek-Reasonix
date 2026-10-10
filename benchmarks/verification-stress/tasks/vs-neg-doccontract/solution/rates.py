def convert(amount, rate):
    """Convert `amount` at `rate` and return the product.

    Both units belong to the caller. A negative amount converts unchanged.
    """
    return amount * rate

def spread(bid, ask):
    """Return `ask - bid`, the spread between the two quoted prices.

    A negative result means the bid is above the ask.
    """
    return ask - bid
