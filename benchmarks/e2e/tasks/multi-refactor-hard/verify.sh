set -e
python3 -m tests.test_pricing
# dead code must be gone
! grep -q "format_legacy" src/receipt.py
# shared helper must exist
grep -q "def compute_discount" src/pricing.py
grep -q "compute_discount(" src/pricing.py
# tax imports from config
grep -q "config" src/tax.py
test -f src/config.py
grep -q "DEFAULT_TAX_RATE" src/config.py
echo OK
