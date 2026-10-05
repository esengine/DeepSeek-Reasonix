def wire_fee(amount):
    if amount <= 0:
        return 0.0
    return max(15.0, amount * 0.001)
