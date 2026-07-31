import sys, os
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))
from src.inventory import check_stock
from src.order import process_order

def test_zero_stock_out():
    assert check_stock({"item": "x", "quantity": 0}) is False, "zero stock must be out"

def test_positive_stock_in():
    assert check_stock({"item": "x", "quantity": 5}) is True

def test_order_attribute_access():
    class Order:
        def __init__(self):
            self.item = "widget"
            self.quantity = 3
    result = process_order(Order())
    assert result["qty"] == 3

def test_order_dict_access():
    result = process_order({"item": "widget", "quantity": 7})
    assert result["qty"] == 7

if __name__ == "__main__":
    test_zero_stock_out()
    test_positive_stock_in()
    test_order_attribute_access()
    test_order_dict_access()
    print("ALL TESTS PASS")
