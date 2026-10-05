#!/usr/bin/env bash
# Grader for the finmath half-even rounding task.
# Runs from a directory that holds the working tree at the top level.
set -euo pipefail

cd "$(dirname "$0")"

python3 - <<'PY'
import unittest
from decimal import Decimal, ROUND_HALF_EVEN

import display
import ledger

CENTS = Decimal("0.01")


def expected(raw):
    """Half-even rounding of the exact decimal given as a string."""
    return Decimal(raw).quantize(CENTS, rounding=ROUND_HALF_EVEN)


HALF_CENT_CASES = [
    "0.125", "0.135", "0.145",
    "1.225", "1.235",
    "2.675",
    "12.345", "12.355",
    "99.995", "1234.565",
]

MISC_CASES = ["0.10", "1.23", "0.999", "4.567", "9.9999", "2.50"]


class TestHalfEvenContract(unittest.TestCase):
    def test_display_rounds_half_even(self):
        for raw in HALF_CENT_CASES:
            self.assertEqual(
                display.format_amount(float(raw)),
                "${}".format(expected(raw)),
                msg=raw,
            )

    def test_ledger_rounds_half_even(self):
        for raw in HALF_CENT_CASES:
            self.assertEqual(
                ledger.round_to_cents(float(raw)),
                float(expected(raw)),
                msg=raw,
            )

    def test_both_modules_agree_on_every_input(self):
        for raw in HALF_CENT_CASES + MISC_CASES:
            want = float(expected(raw))
            self.assertEqual(ledger.round_to_cents(float(raw)), want, msg=raw)
            self.assertEqual(display.format_amount(float(raw)), "${}".format(expected(raw)), msg=raw)

    def test_negative_ties_round_to_even(self):
        self.assertEqual(ledger.round_to_cents(-0.125), -0.12)


if __name__ == "__main__":
    unittest.main(verbosity=2)
PY
