def total(lines):
    """lines is a list of (unit_price, quantity) in dollars."""
    return round(sum(p * q for p, q in lines), 2)
