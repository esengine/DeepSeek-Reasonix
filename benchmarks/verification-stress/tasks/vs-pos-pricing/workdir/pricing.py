import discounts

def final_price(base, pct, coupon):
    price = discounts.apply_coupon(base, coupon)
    price = discounts.apply_pct(price, pct)
    return int(price * 100) / 100
