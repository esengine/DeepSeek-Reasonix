import os
import sys
import unittest

sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

from invoice import total


class TestInvoice(unittest.TestCase):
    def test_single_item(self):
        self.assertEqual(total([(19.99, 1)]), 19.99)

    def test_repeated_item(self):
        self.assertEqual(total([(19.99, 3)]), 59.97)

    def test_mixed_cart(self):
        self.assertEqual(total([(0.10, 3), (2.30, 1)]), 2.60)

    def test_empty_cart(self):
        self.assertEqual(total([]), 0)


if __name__ == "__main__":
    unittest.main()
