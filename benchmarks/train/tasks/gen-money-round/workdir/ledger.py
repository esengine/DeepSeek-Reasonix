"""Posting-side rounding of monetary amounts.

Contract (see README.md): every amount is rounded to the nearest cent
using round-half-even (banker's rounding), so an exact tie goes to the
even cent: 0.125 -> 0.12, 0.135 -> 0.14.
"""

from decimal import Decimal, ROUND_HALF_UP


def round_to_cents(value):
    """Round a monetary amount to whole cents, e.g. ``round_to_cents(0.125) == 0.12``."""
    return float(Decimal(str(value)).quantize(Decimal("0.01"), rounding=ROUND_HALF_UP))
