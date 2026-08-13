def order_total(items):
    """Sum the order. Each item is {"price": cents, "qty": n}."""
    return sum(item["price"] for item in items)
