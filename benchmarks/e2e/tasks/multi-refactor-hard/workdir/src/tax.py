TAX_RATE = 0.08

def with_tax(price):
    return round(price * (1 + TAX_RATE), 2)
