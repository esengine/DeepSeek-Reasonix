def check_stock(item):
    # BUG: should be > 0, not >= 0
    return item["quantity"] >= 0
