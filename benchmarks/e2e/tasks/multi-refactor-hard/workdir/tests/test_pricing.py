import sys, os
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from src.pricing import apply_coupon, apply_bulk_discount, total_with_discount
from src.tax import with_tax, TAX_RATE
from src.receipt import format_receipt

def test_coupon():
    assert apply_coupon(100.0, 0.15) == 15.0

def test_coupon_rounding():
    assert apply_coupon(99.99, 0.15) == round(99.99 * 0.15, 2)

def test_bulk_10():
    assert apply_bulk_discount(50.0, 10) == 10.0

def test_bulk_5():
    assert apply_bulk_discount(50.0, 5) == 5.0

def test_bulk_2():
    assert apply_bulk_discount(50.0, 2) == 0.0

def test_total():
    assert total_with_discount(100.0, 0.1) == 90.0

def test_tax():
    assert with_tax(100.0) == 108.0
    assert TAX_RATE == 0.08

def test_receipt():
    r = format_receipt([("a", 1.5), ("b", 2.5)], 4.0)
    assert "TOTAL: 4.00" in r and "a: 1.50" in r

if __name__ == "__main__":
    test_coupon(); test_coupon_rounding(); test_bulk_10(); test_bulk_5()
    test_bulk_2(); test_total(); test_tax(); test_receipt()
    print("ALL REFACTOR TESTS PASS")
