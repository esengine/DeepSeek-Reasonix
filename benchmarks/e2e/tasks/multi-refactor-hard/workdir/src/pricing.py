def compute_discount(price, rate):
    return round(price * rate, 2)

def apply_coupon(price, coupon_rate):
    # duplicate 1
    return round(price * coupon_rate, 2)

def apply_bulk_discount(price, qty):
    if qty >= 10:
        rate = 0.2
    elif qty >= 5:
        rate = 0.1
    else:
        rate = 0.0
    # duplicate 2
    return round(price * rate, 2)

def total_with_discount(price, rate):
    return price - compute_discount(price, rate)
