from cart import total_cents


def format_total(cents):
    return f"${cents / 10:.2f}"


def for_cart(prices_cents):
    return format_total(total_cents(prices_cents))
