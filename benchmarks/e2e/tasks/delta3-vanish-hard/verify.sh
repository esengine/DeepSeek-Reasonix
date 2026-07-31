set -e
python3 - <<'PY'
import sys, os, itertools
sys.path.insert(0, os.getcwd())
import delta
d = delta.delta
for s in itertools.product([0,1,2], repeat=3):
    d1 = d(s); d2 = d(d1); d3 = d(d2)
    assert d3 == (0, 0, 0), f"Δ³ not zero for {s}: {d3}"
# integer cyclic Δ³ counterexample on (0,0,0,1) with length-4 cyclic wrap
def idelta(f):
    n = len(f)
    return tuple((f[(i+1) % n] - f[i]) for i in range(n))
s4 = (0, 0, 0, 1)
d3int = idelta(idelta(idelta(s4)))
assert any(x != 0 for x in d3int), f"integer Δ³ should be nonzero: {d3int}"
ans = open("answer.txt").read().strip()
assert ans == "27", f"answer.txt: {ans}"
print("Δ³≡0 verified for all 27 length-3 seqs mod 3; integer counterexample exists")
PY
echo OK
