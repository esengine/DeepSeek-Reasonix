def process_order(order):
    # BUG: order comes as an object with .quantity attribute
    qty = order["quantity"]
    return {"item": order["item"], "qty": qty}
