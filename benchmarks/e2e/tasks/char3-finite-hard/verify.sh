set -e
python3 - <<'PY'
import sys, os
sys.path.insert(0, os.getcwd())
import char3
# GF(3): 1+1+1 = 0
assert char3.gf3_add(1, char3.gf3_add(1, 1)) == 0
# x³ ≡ x for all 3 elements
for x in range(3):
    assert pow(x, 3, 3) == x % 3, f"x³≠x for {x}"
# over Q: x³-x ≠ 0 identically
assert 2**3 - 2 != 0
# embedding must be impossible; embed_check() returns False
assert char3.embed_check() is False, "embed_check must return False (impossible)"
ans = open("answer.txt").read().strip()
assert ans == "3", f"answer.txt: {ans}"
print("char3 verified: 1+1+1=0, x³≡x on 3 elements, embedding impossible")
PY
echo OK
