set -e
python3 - <<'PY'
import calc
assert calc.add(2, 3) == 5, calc.add(2, 3)
assert calc.add(10, 5) == 15, calc.add(10, 5)
assert calc.mul(2, 3) == 6, calc.mul(2, 3)
PY
