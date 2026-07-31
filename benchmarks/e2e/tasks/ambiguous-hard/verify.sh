set -e
python3 - <<'PY'
import sys, os
sys.path.insert(0, os.getcwd())
from analysis import net_revenue_by_product, losing_products
nr = net_revenue_by_product("data/sales.csv", "data/returns.csv")
# widget: 10+5+8=23 units * 5 = 115; returns: 2026-06-01 excluded (old), 07-12 3 units * 5 * 1.15 = 17.25 → 97.75
# gadget: 4+6=10 * 20 = 200; return 07-22 1 * 20 * 1.15 = 23 → 177
# doohickey: 2 * 15 = 30
assert abs(nr["widget"] - 97.75) < 0.01, nr
assert abs(nr["gadget"] - 177.0) < 0.01, nr
assert abs(nr["doohickey"] - 30.0) < 0.01, nr
assert losing_products(nr) == [], "all products profitable here"
# NOTES.md must exist with interpretation
assert os.path.exists("NOTES.md")
assert len(open("NOTES.md").read().strip()) > 40, "NOTES.md too short"
print("OK")
PY
