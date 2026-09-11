def apply_pct(price, pct):
    return price * (1 - pct / 100.0)

def apply_coupon(price, coupon):
    return price - coupon
