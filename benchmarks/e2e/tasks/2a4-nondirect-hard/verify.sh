set -e
python3 - <<'PY'
import sys, os
sys.path.insert(0, os.getcwd())
import group
bt = group.binary_tetrahedral()
az = group.a4_times_z2()
assert len(bt) == 24, f"2A4 size {len(bt)}"
assert len(az) == 24, f"A4xZ2 size {len(az)}"
o2_bt = sum(1 for g in bt if group.order_of(bt, g) == 2)
o2_az = sum(1 for g in az if group.order_of(az, g) == 2)
assert o2_bt == 1, f"2A4 order-2 = {o2_bt}, want 1 (only -1)"
assert o2_az >= 4, f"A4xZ2 order-2 = {o2_az}, want >= 4"
assert o2_bt != o2_az, "counts differ → non-isomorphic"
print(f"2A4:{o2_bt} AXZ2:{o2_az} → non-isomorphic OK")
PY
echo OK
