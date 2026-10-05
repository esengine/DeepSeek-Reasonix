"""Contract tests for the finmath library.

Run with:  python3 -m unittest discover -s tests
"""

import unittest
from decimal import Decimal, ROUND_HALF_EVEN

import display
import ledger

CASES = ["0.125", "0.135", "1.225", "1.235", "2.675", "12.345", "12.355"]


def expected(raw):
    return Decimal(raw).quantize(Decimal("0.01"), rounding=ROUND_HALF_EVEN)


class TestHalfEvenRounding(unittest.TestCase):
    def test_display_rounds_half_even(self):
        for raw in CASES:
            self.assertEqual(
                display.format_amount(float(raw)),
                "${}".format(expected(raw)),
                msg=raw,
            )

    def test_ledger_rounds_half_even(self):
        for raw in CASES:
            self.assertEqual(
                ledger.round_to_cents(float(raw)),
                float(expected(raw)),
                msg=raw,
            )


if __name__ == "__main__":
    unittest.main()
