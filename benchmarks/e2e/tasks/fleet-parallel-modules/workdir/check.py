import sys
sys.path[:0] = ["billing", "shipping", "notify"]
from total import order_total
from rate import rate_cents
from subject import subject

failures = []
if order_total([{"price": 250, "qty": 2}, {"price": 100, "qty": 3}]) != 800:
    failures.append("billing")
if rate_cents(1000) != 500 or rate_cents(1001) != 1000 or rate_cents(1) != 500:
    failures.append("shipping")
if subject("Ada") != "Hi, Ada!":
    failures.append("notify")
if failures:
    print("FAIL:", ", ".join(failures)); sys.exit(1)
print("OK")
