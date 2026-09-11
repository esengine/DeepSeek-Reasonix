def line_total(price, qty):
    return price * qty


def total(items):
    """Sum of price*qty over (price, qty) items, in currency units."""
    amount = 0.0
    for price, qty in items:
        amount += line_total(price, qty)
    return amount
