"""Human-facing display of monetary amounts.

Contract (see README.md): every amount is rounded to the nearest cent
using round-half-even (banker's rounding), so an exact tie goes to the
even cent: 0.125 -> 0.12, 0.135 -> 0.14.
"""

from decimal import Decimal, ROUND_HALF_EVEN


def format_amount(value):
    """Format a monetary amount as ``"$<cents>"``, e.g. ``format_amount(0.135) == "$0.14"``."""
    rounded = Decimal(str(value)).quantize(Decimal("0.01"), rounding=ROUND_HALF_EVEN)
    return "${}".format(rounded)
