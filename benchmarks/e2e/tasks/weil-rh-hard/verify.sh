set -e
python3 weil.py > /tmp/weil-out.txt 2>&1 || { cat /tmp/weil-out.txt; exit 1; }
grep -q "ALL PASS" /tmp/weil-out.txt
python3 - <<'PY'
import math
ans = open("answer.txt").read().strip()
want = str(1/math.sqrt(3))
assert abs(float(ans) - float(want)) < 1e-12, f"answer.txt: {ans}, want {want}"
print("Weil RH root modulus verified: 1/sqrt(3)")
PY
echo OK
