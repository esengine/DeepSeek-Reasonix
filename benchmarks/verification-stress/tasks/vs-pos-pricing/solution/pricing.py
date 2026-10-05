import discounts


def final_price(base, pct, coupon):
    price = discounts.apply_pct(base, pct)
    price = discounts.apply_coupon(price, coupon)
    return int(max(price, 0.0) * 100) / 100
