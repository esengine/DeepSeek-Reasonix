# finmath

A small library that rounds monetary amounts to whole cents.

## Rounding contract

Every amount is rounded to the nearest cent using **round-half-even**
(banker's rounding): an amount that falls exactly halfway between two
cents goes to the *even* cent.

| input   | rounded |
|---------|---------|
| 0.125   | 0.12    |
| 0.135   | 0.14    |
| 1.225   | 1.22    |
| 1.235   | 1.24    |
| 2.675   | 2.68    |

## Modules

- `display.py` — `format_amount(value) -> str` formats an amount for
  display, e.g. `format_amount(0.135) == "$0.14"`.
- `ledger.py` — `round_to_cents(value) -> float` rounds an amount for
  posting, e.g. `round_to_cents(0.125) == 0.12`.

Both modules must round **every** input identically to the contract
above; they must never disagree with each other.

## Tests

Run the contract tests with:

    python3 -m unittest discover -s tests
