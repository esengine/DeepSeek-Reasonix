set -e
python3 - <<'PY'
import sys, os
sys.path.insert(0, os.getcwd())
import crt
rec, proj = crt.crt_reconstruct, crt.crt_project
# round trip all 60 triples
for r1 in range(3):
    for r2 in range(4):
        for r3 in range(5):
            x = rec(r1, r2, r3)
            assert proj(x) == (r1, r2, r3), f"project failed ({r1},{r2},{r3}) → {x}"
# reverse round trip all 60 x
for x in range(60):
    assert rec(*proj(x)) == x, f"reconstruct failed {x}"
ans = open("answer.txt").read().strip()
assert ans == "60", f"answer.txt: {ans}"
print("CRT round-trip verified for all 60 triples and all 60 x")
PY
echo OK
